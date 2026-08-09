package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

type verifiedDownloadProgress struct {
	Fraction float64
	Received int64
	Total    int64
}

func downloadVerifiedFile(url, destination, expectedSHA256 string, onProgress func(verifiedDownloadProgress)) error {
	if digest, err := fileSHA256(destination); err == nil && digest == expectedSHA256 {
		if onProgress != nil {
			size := int64(0)
			if info, statErr := os.Stat(destination); statErr == nil {
				size = info.Size()
			}
			onProgress(verifiedDownloadProgress{Fraction: 1, Received: size, Total: size})
		}
		return nil
	}
	partial := destination + ".partial"
	_ = os.Remove(partial)
	request, err := http.NewRequestWithContext(processCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download failed with HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	reader := io.TeeReader(response.Body, hash)
	buffer := make([]byte, 128*1024)
	var received int64
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if _, err := file.Write(buffer[:count]); err != nil {
				file.Close()
				_ = os.Remove(partial)
				return err
			}
			received += int64(count)
			if onProgress != nil {
				fraction := -1.0
				if response.ContentLength > 0 {
					fraction = float64(received) / float64(response.ContentLength)
				}
				onProgress(verifiedDownloadProgress{
					Fraction: fraction,
					Received: received,
					Total:    response.ContentLength,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			file.Close()
			_ = os.Remove(partial)
			return readErr
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		_ = os.Remove(partial)
		return errors.New("downloaded file does not match its reviewed SHA-256 digest")
	}
	_ = os.Remove(destination)
	return os.Rename(partial, destination)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
