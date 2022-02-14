package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	gmux "github.com/gorilla/mux"

	"github.com/google/go-github/v42/github"
)

var backoffSchedule = []time.Duration{
	10 * time.Second,
	15 * time.Second,
	20 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

var messageClient mqttClient

type UpdaterPayload struct {
	SHA                string `json:"sha"`
	Repository         string `json:"repository"`
	ArtifactName       string `json:"artifact_name"`
	ArchiveDownloadURL string `json:"archive_download_url"`
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

	if updaterPayload.Repository == "" || updaterPayload.ArtifactName == "" || updaterPayload.SHA == "" {
		fmt.Println("empty field(s) found in payload:", updaterPayload)
		http.Error(w, "Error parsing request", http.StatusBadRequest)
		return
	}

	fmt.Println(fmt.Sprintf("Received new artifact published event for repository %s", updaterPayload.Repository))

	url, err := processDeployMessage(updaterPayload)
	if err != nil {
		http.Error(w, "Error parsing request", http.StatusBadRequest)
		return
	}
	updaterPayload.ArchiveDownloadURL = url
	json, _ := json.Marshal(updaterPayload)

	messageClient.PublishPushTopic(string(json))

	fmt.Fprintf(w, "{\"status\":\"success\"}")
}

func processDeployMessage(up UpdaterPayload) (string, error) {
	url, err := getDownloadURLWithRetries(up)
	if err != nil {
		return "", err
	}
	return url, nil
}

func getDownloadURLWithRetries(updaterPayload UpdaterPayload) (string, error) {
	var err error
	var url string
	for _, backoff := range backoffSchedule {
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
		if updaterPayload.ArtifactName == a.GetName() {
			return a.GetArchiveDownloadURL(), nil
		}
	}

	return "", fmt.Errorf("no artifact found for %s", updaterPayload.ArtifactName)
}

func main() {
	srvAddr := fmt.Sprintf("0.0.0.0:%s", os.Getenv("PORT"))

	// TODO: reusing another app's mqtt instance to save cost. Once viable MVP finished I can provision a dedicated instance
	// TODO: read/write user is fine for this app, but clients will need read only
	user := os.Getenv("CLOUDMQTT_USER")
	pw := os.Getenv("CLOUDMQTT_PASSWORD")
	url := os.Getenv("CLOUDMQTT_URL")
	mqttAddr := fmt.Sprintf("mqtt://%s:%s@%s", user, pw, url)

	messageClient = newMQTTClient(mqttAddr)
	messageClient.SubscribePushTopic(func(message string) {
		var payload UpdaterPayload
		err := json.Unmarshal([]byte(message), &payload)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(fmt.Sprintf("Received message on topic %s: %s", pushTopic, payload))
		}

	})

	router := gmux.NewRouter().StrictSlash(true)
	router.Handle("/push", requireLogin(http.HandlerFunc(handleWebhook))).Methods("POST")

	srv := &http.Server{
		Handler: router,
		Addr:    srvAddr,
	}

	log.Println("server started on ", srvAddr)
	logger.Fatal(srv.ListenAndServe())
}

func requireLogin(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, req *http.Request) {
		if !isAuthenticated(req) {
			fmt.Print("unauthenticated request", req.Header)
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, req)
	}
	return http.HandlerFunc(fn)
}

func isAuthenticated(req *http.Request) bool {
	allowedApiKey := os.Getenv("PI_APP_UPDATER_API_KEY")
	apiKey := req.Header.Get("api-key")
	if apiKey == "" {
		return false
	}
	if apiKey != allowedApiKey {
		return false
	}
	return true
}
