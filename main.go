package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/charmbracelet/log"
	"github.com/sweepies/tok-dl/cache"
	"github.com/sweepies/tok-dl/tikwm"
)

func main() {

	logger := log.New(os.Stderr)
	secretToken := os.Getenv("SECRET_TOKEN")
	if secretToken == "" {
		logger.Fatal("SECRET_TOKEN environment variable is not set!")
	}

	c := cache.New("/app/cache")

	caller := tikwm.New(c, logger)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Health check")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer "+secretToken {
			logger.Warn("Unauthorized access attempt", "ip", r.RemoteAddr)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		tiktokURL := r.URL.Query().Get("url")
		if tiktokURL == "" {
			http.Error(w, "Missing url parameter", http.StatusBadRequest)
			return
		}

		logger.Info("Processing request", "url", tiktokURL)

		metadata, err := caller.FetchMetadata(tiktokURL)
		if err != nil {
			logger.Error("Metadata fetch failed", "error", err)
			http.Error(w, "Failed to fetch metadata", http.StatusInternalServerError)
			return
		}

		if len(metadata.Data.Images) > 0 {
			logger.Info("Detected Gallery", "count", len(metadata.Data.Images))

			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", "attachment; filename=\"gallery.zip\"")
			w.Header().Set("Transfer-Encoding", "chunked")

			zw := zip.NewWriter(w)
			for i, imgURL := range metadata.Data.Images {
				resp, err := http.Get(imgURL)
				if err != nil {
					logger.Error("Failed to fetch image", "url", imgURL, "err", err)
					continue
				}

				fileName := fmt.Sprintf("image_%d.jpg", i+1)
				f, err := zw.Create(fileName)
				if err != nil {
					logger.Error("Failed to create zip", "f", f, "fileName", fileName)
					resp.Body.Close()
					continue
				}

				_, err = io.Copy(f, resp.Body)
				resp.Body.Close()
				if err != nil {
					logger.Error("Error copying image to zip", "file", fileName, "err", err)
				}
			}
			zw.Close()
			return
		}

		resp, err := http.Get(metadata.Data.Play)
		if err != nil {
			http.Error(w, "CDN error", 502)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Disposition", "attachment; filename=\"video.mp4\"")
		io.Copy(w, resp.Body)

		if metadata.Data.Play == "" {
			http.Error(w, "No video URL found", http.StatusNotFound)
			return
		}
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			logger.Error("Stream interrupted", "error", err)
		}
	})

	logger.Info("Server starting", "port", 8080)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
