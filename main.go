package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/google/go-github/v32/github"
)

type server struct{}

type e struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

type resp struct {
	Errors []e `json:"errors"`
}

func Error(w http.ResponseWriter, err error) {
	code := "MANIFEST_UNKNOWN"
	httpCode := http.StatusNotFound
	if terr, ok := err.(*transport.Error); ok {
		http.Error(w, "", terr.StatusCode)
		json.NewEncoder(w).Encode(terr.Errors)
		return
	}

	http.Error(w, "", httpCode)
	json.NewEncoder(w).Encode(&resp{
		Errors: []e{{
			Code: code, Message: err.Error()}},
	})
}

var blobs = regexp.MustCompile("(.+)(blobs|manifests)\\/(sha256:.+)\\/?")
var manifests = regexp.MustCompile("(.+)/manifests\\/?(.+)?")

func main() {
	http.Handle("/v2/", &server{})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}
	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	path := strings.TrimPrefix(r.URL.String(), "/v2/")

	switch {
	case path == "":
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	case blobs.MatchString(path):
		name := blobs.FindStringSubmatch(path)[1]
		parts := strings.Split(name, "/")
		resource := blobs.FindStringSubmatch(path)[2]
		digest := blobs.FindStringSubmatch(path)[3]

		url := fmt.Sprintf("http://humblegeoffrey:5000/v2/%s/%s/%s/%s", parts[0], parts[1], resource, digest)
		http.Redirect(w, r, url, http.StatusSeeOther)

	case manifests.MatchString(path):
		_, err := buildImage(ctx, path, w, r)
		if err != nil {
			Error(w, err)
		}
	default:
		Error(w, fmt.Errorf("unimplemented (%s)", path))
	}
}

func buildImage(ctx context.Context, path string, w http.ResponseWriter, r *http.Request) (bool, error) {
	ghclient := github.NewClient(nil)
	parts := strings.Split(path, "/")

	user := parts[0]
	repo := parts[1]
	var dirPath string
	if len(parts) > 3 {
		dirPath = strings.Join(parts[2:len(parts)-2], "/")
	}
	ref := parts[len(parts)-1]

	var revision string
	switch ref {
	case "latest", "manifests", "":
		repo, _, err := ghclient.Repositories.Get(ctx, user, repo)
		if err != nil {
			return false, err
		}
		revision = repo.GetDefaultBranch()
	default:
		revision = ref
	}

	gitSource := fmt.Sprintf("https://github.com/%s/%s.git#%s", user, repo, revision)

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return false, err
	}

	name := fmt.Sprintf("humblegeoffrey:5000/%s/%s", user, repo)
	tag := fmt.Sprintf("%s:%s", name, revision)

	opts := types.ImageBuildOptions{
		Version:       types.BuilderBuildKit,
		Tags:          []string{tag},
		RemoteContext: gitSource,
	}
	if dirPath != "" {
		opts.Dockerfile = strings.Replace(dirPath, "dockerfile", "Dockerfile", -1)
	}

	imageBuildRes, err := cli.ImageBuild(ctx, nil, opts)
	if err != nil {
		return false, err
	}

	_, err = io.Copy(os.Stdout, imageBuildRes.Body)

	defer imageBuildRes.Body.Close()

	if err != nil {
		return false, err
	}

	imagePushRes, err := cli.ImagePush(ctx, tag, types.ImagePushOptions{
		All:          true,
		RegistryAuth: "123",
	})
	if err != nil {
		return false, err
	}

	defer imagePushRes.Close()

	_, err = io.Copy(os.Stdout, imagePushRes)
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf("http://humblegeoffrey:5000/v2/%s/%s/manifests/%s", user, repo, revision)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	return false, nil
}
