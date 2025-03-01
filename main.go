package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/blossom"
	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
)

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("Error loading .env file")
		return
	}

	relay := setupRelay()
	db := setupDatabase()

	// Configure event handling
	configureRelayEventHandling(relay, db)

	// Blossom setup
	bl := setupBlossom(relay, db)

	// Configure Blossom rules
	configureBlossomRules(bl, db)

	// Run the server
	serverAddress := getEnv("SERVER_ADDRESS", "0.0.0.0:3334")
	fmt.Printf("Running on %s\n", serverAddress)
	if err := http.ListenAndServe(serverAddress, relay); err != nil {
		panic(err)
	}
}

func setupRelay() *khatru.Relay {
	relay := khatru.NewRelay()
	relay.Info.Name = getEnv("RELAY_NAME", "my relay")
	relay.Info.PubKey = getEnv("RELAY_PUBKEY", "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	relay.Info.Description = getEnv("RELAY_DESCRIPTION", "this is my custom relay")
	relay.Info.Icon = getEnv("RELAY_ICON_URL", "https://example.com/icon.jpg")
	return relay
}

func setupDatabase() *badger.BadgerBackend {
	dbPath := getEnv("BADGER_DB_PATH", "/tmp/khatru-badger-tmp")
	db := &badger.BadgerBackend{Path: dbPath}
	if err := db.Init(); err != nil {
		panic(err)
	}
	return db
}

func configureRelayEventHandling(relay *khatru.Relay, db *badger.BadgerBackend) {
	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, db.QueryEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.ReplaceEvent = append(relay.ReplaceEvent, db.ReplaceEvent)

	relay.RejectEvent = append(relay.RejectEvent, func(ctx context.Context, event *nostr.Event) (bool, string) {
		if event.Kind != nostr.KindFileMetadata {
			return true, "Only file metadata events are allowed"
		}
		allowedUsers := getAllowedUsers()
		if !allowedUsers[event.PubKey] {
			return true, "Unauthorized pubkey"
		}
		return false, ""
	})
}

func setupBlossom(relay *khatru.Relay, db *badger.BadgerBackend) *blossom.BlossomServer {
	bl := blossom.New(relay, "http://0.0.0.0:3334")
	bl.Store = blossom.EventStoreBlobIndexWrapper{Store: db, ServiceURL: bl.ServiceURL}
	return bl
}

func configureBlossomRules(bl *blossom.BlossomServer, db *badger.BadgerBackend) {
	bl.RejectGet = append(bl.RejectGet, rejectGetHandler(bl, db))
	bl.RejectUpload = append(bl.RejectUpload, rejectUploadHandler())

	bl.StoreBlob = append(bl.StoreBlob, storeBlobHandler)
	bl.LoadBlob = append(bl.LoadBlob, loadBlobHandler)
	bl.DeleteBlob = append(bl.DeleteBlob, deleteBlobHandler)
}

func rejectGetHandler(bl *blossom.BlossomServer, db *badger.BadgerBackend) func(context.Context, *nostr.Event, string) (bool, string, int) {
    return func(ctx context.Context, auth *nostr.Event, sha256 string) (bool, string, int) {
        // Reject GET requests based on specific conditions
        fmt.Println("Checking authorization for GET request.")
        if auth == nil {
            return true, "Authorization event is missing", 403
        }

        filter := nostr.Filter{
            Tags: nostr.TagMap{
                "x": []string{sha256},
            },
            Kinds: []int{1063},
        }

        eventChan, err := db.QueryEvents(ctx, filter)
        if err != nil {
            return true, fmt.Sprintf("Error querying events: %s", err.Error()), 500
        }

        var events []*nostr.Event
        for event := range eventChan {
            events = append(events, event)
        }

        events = filterOnlyLatestEventForEachAuthor(events)

        fmt.Printf(" - found %d matches\n", len(events))
        for _, event := range events {
            if isPubkeyTaggedInEvent(event, auth.PubKey) && pubkeyOwnsBlob(bl, ctx, event.PubKey, sha256) {
                return false, "", 200
            }
        }
        return true, "Unauthorized access or no associated event found", 403
    }
}

func filterOnlyLatestEventForEachAuthor(events []*nostr.Event) []*nostr.Event {
	latestEvents := make(map[string]*nostr.Event)
	for _, event := range events {
		currentEvent, exists := latestEvents[event.PubKey]
		if !exists || currentEvent.CreatedAt < event.CreatedAt {
			latestEvents[event.PubKey] = event
		}
	}
	events = nil // Clear current slice
	for _, event := range latestEvents {
		events = append(events, event)
	}
	return events
}

func isPubkeyTaggedInEvent(event *nostr.Event, pubkey string) bool {
	for _, tag := range event.Tags {
		if tag[0] == "p" && tag[1] == pubkey {
			return true
		}
	}
	return false
}

func pubkeyOwnsBlob(bl *blossom.BlossomServer, ctx context.Context, pubkey string, sha256 string) bool {
	blobDescriptors, err := bl.Store.List(ctx, pubkey)
	if err != nil {
		return false
	}

	for blobDescriptor := range blobDescriptors {
		if blobDescriptor.SHA256 == sha256 {
			return true
		}
	}
	return false
}


func rejectUploadHandler() func(context.Context, *nostr.Event, int, string) (bool, string, int) {
	return func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		fmt.Println("Received upload")
		if size > int(getMaxFileSize()) {
			fmt.Println("file too large")
			return true, "file too large", 413
		}
		if auth == nil || !getAllowedUsers()[auth.PubKey] {
			fmt.Println("user not authorized")
			return true, "unauthorized", 403
		}
		fmt.Println("upload ok to proceed")
		return false, "", 200
	}
}

func storeBlobHandler(ctx context.Context, sha256 string, body []byte) error {
	fmt.Println("storing blob", sha256)
	blobPath := filepath.Join(getBlobDir(), sha256)
	if err := os.MkdirAll(getBlobDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(blobPath, body, 0644)
}

func loadBlobHandler(ctx context.Context, sha256 string) (io.ReadSeeker, error) {
	blobPath := filepath.Join(getBlobDir(), sha256)
	file, err := os.Open(blobPath)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func deleteBlobHandler(ctx context.Context, sha256 string) error {
	blobPath := filepath.Join(getBlobDir(), sha256)
	if err := os.Remove(blobPath); err != nil {
		return err
	}
	return nil
}

func getMaxFileSize() int {
	return getEnvAsInt("MAX_FILE_SIZE", 10 * 1024 * 1024)
}

func getBlobDir() string {
	return getEnv("BLOB_DIRECTORY", "blobs")
}

func getEnvAsInt(key string, fallback int) int {
	if valueStr, exists := os.LookupEnv(key); exists {
		if value, err := strconv.Atoi(valueStr); err == nil {
			return value
		}
	}
	return fallback
}

func getAllowedUsers() map[string]bool {
	envValue := getEnv("ALLOWED_USERS", "")
	if envValue == "" {
		return map[string]bool{}
	}

	allowedUsersMap := make(map[string]bool)
	users := strings.Split(envValue, ",")
	for _, user := range users {
		allowedUsersMap[user] = true
	}
	return allowedUsersMap
}

// Utility function to get environment variables with a fallback value
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
