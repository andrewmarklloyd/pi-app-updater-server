package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/go-github/v42/github"
)

var backoffSchedule = []time.Duration{
	10 * time.Second,
	15 * time.Second,
	20 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

type UpdaterPayload struct {
	SHA        string `json:"sha"`
	Repository string `json:"repository"`
	Artifact   string `json:"artifact"`
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("error reading request body: err=%s\n", err)
		return
	}
	defer r.Body.Close()

	var updaterPayload UpdaterPayload
	err = json.Unmarshal(payload, &updaterPayload)
	if err != nil {
		http.Error(w, "Error parsing request", http.StatusBadRequest)
		return
	}

	err = processDeployMessage(updaterPayload)
	if err != nil {
		http.Error(w, "Error parsing request", http.StatusBadRequest)
		return
	}

	fmt.Fprintf(w, "{\"status\":\"success\"}")
}

func processDeployMessage(up UpdaterPayload) error {
	url, err := getDownloadURLWithRetries(up)
	if err != nil {
		return err
	}
	fmt.Println(url)
	return nil
}

func getDownloadURLWithRetries(updaterPayload UpdaterPayload) (string, error) {
	var err error
	var url string
	for _, backoff := range backoffSchedule {
		fmt.Println("trying to get download url")
		url, err = getDownloadURL(updaterPayload)
		if url != "" {
			return url, nil
		}

		fmt.Println(fmt.Sprintf("Retrying in %v", backoff))
		time.Sleep(backoff)
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("an unexpected event occurred, no url found and no error returned")
}

func getDownloadURL(updaterPayload UpdaterPayload) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/actions/artifacts", updaterPayload.Repository), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var artifacts github.ArtifactList
	err = json.Unmarshal(body, &artifacts)
	if err != nil {
		return "", err
	}

	for _, a := range artifacts.Artifacts {
		if updaterPayload.Artifact == a.GetName() {
			return a.GetArchiveDownloadURL(), nil
		}
	}

	return "", fmt.Errorf("no artifact found for %s", updaterPayload.Artifact)
}

func main() {
	srvAddr := fmt.Sprintf("0.0.0.0:%s", os.Getenv("PORT"))
	log.Println("server started on ", srvAddr)
	// todo: add auth
	http.HandleFunc("/push", handleWebhook)
	log.Fatal(http.ListenAndServe(srvAddr, nil))
}
