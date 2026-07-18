package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Pack struct {
	ID int `yaml:"id"`
}

type PacksFile struct {
	Packs []Pack `yaml:"packs"`
}

type PackStatus struct {
	ID     int    `yaml:"id"`
	Name   string `yaml:"name,omitempty"`
	Status string `yaml:"status,omitempty"`
	Reason string `yaml:"reason,omitempty"`
}

func isFileLocked(lockFilePath string) bool {
	// If the file does not exist, create and go on
	readPid, err := os.ReadFile(lockFilePath)
	if os.IsNotExist(err) {
		pid := os.Getpid()
		err := os.WriteFile(lockFilePath, []byte(strconv.Itoa(pid)), 0644)
		if err != nil {
			log.Printf("Error locking: %v", err)
			os.Exit(1)
		}
		return false
	}

	// If the process is running => return an error
	// Otherwise: Write your PID and continue
	_, err = os.Stat(fmt.Sprintf("/proc/%v", strings.TrimSpace(string(readPid))))
	if err == nil {
		return true
	} else {
		pid := os.Getpid()
		err := os.WriteFile(lockFilePath, []byte(strconv.Itoa(pid)), 0644)
		if err != nil {
			log.Printf("Error locking: %v", err)
			os.Exit(1)
		}
		return false
	}
}

func main() {
	packmanDir := os.Getenv("PACKMAN_DIR")
	if packmanDir == "" {
		fmt.Fprintln(os.Stderr, "Error: PACKMAN_DIR environment variable not set")
		os.Exit(1)
	}

	// Ensure the packman folder exists
	if _, err := os.Stat(packmanDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "PACKMAN_DIR (%s) not found\n", packmanDir)
		os.Exit(1)
	}

	lockFilePath := filepath.Join(packmanDir, ".packman-lock")
	desiredPackPath := filepath.Join(packmanDir, "packs.yaml")
	packStatusDB := filepath.Join(packmanDir, "pack-status.yaml")

	// Only one instance of the program can run at the same time
	isLocked := isFileLocked(lockFilePath)
	if isLocked {
		fmt.Printf("Only one instance of packman can run at the same time")
		os.Exit(1)
	}

	smPackSearchURL := os.Getenv("SM_PACK_SEARCH_URL")
	if smPackSearchURL == "" {
		fmt.Fprintln(os.Stderr, "Error: SM_PACK_SEARCH_URL environment variable not set")
		os.Exit(1)
	}

	packFolder := os.Getenv("PACK_FOLDER")
	if packFolder == "" {
		fmt.Fprintln(os.Stderr, "Error: PACK_FOLDER environment variable not set")
		os.Exit(1)
	}

	// Download pack.yaml from an http source
	packYamlUrl := os.Getenv("PACK_YAML_URL")
	if packYamlUrl != "" {
		err := downloadPackYaml(desiredPackPath, packYamlUrl)
		if err != nil {
			log.Printf("downloadPackYaml error: %v\n", err)
			os.Exit(1)
		}
	}

	// Ensure the pack folder exists
	if _, err := os.Stat(packFolder); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Cannot access the directory [%s] (%v)\n", packFolder, err)
		os.Exit(1)
	}

	// Load packs from packs.yaml
	desiredPacks, err := loadPacks(desiredPackPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading packs: %v\n", err)
		os.Exit(1)
	}

	// Create pack-status.yaml if it does not exist
	statusPath := filepath.Join(filepath.Dir(packStatusDB), "pack-status.yaml")
	_, err = os.Stat(statusPath)
	if os.IsNotExist(err) {
		if err := writePackStatusDB([]PackStatus{}, statusPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating pack-status.yaml: %v\n", err)
			os.Exit(1)
		}
	}

	// Load existing managedPacks
	managedPacks, err := loadPackStatuses(statusPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading managed packs file: %v\n", err)
		os.Exit(1)
	}

	downloadWantedPacks(desiredPacks, managedPacks, packFolder, smPackSearchURL)
	removeUnwantedPacks(desiredPacks, managedPacks, packFolder)
	reconcileManagedPacks(managedPacks, packFolder)

	// Convert managedPacksDB map to a slice
	managedPackSlice := make([]PackStatus, 0, len(desiredPacks))
	for _, p := range managedPacks {
		managedPackSlice = append(managedPackSlice, p)
	}

	// Save the updated status file
	if err := writePackStatusDB(managedPackSlice, statusPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving managedPacks: %v\n", err)
		os.Exit(1)
	}
}

// Check if the packs are deleted in packfolder
func reconcileManagedPacks(managedPacks map[int]PackStatus, packFolder string) {
	for id, pack := range managedPacks {
		packPath := filepath.Join(packFolder, pack.Name)

		info, err := os.Stat(packPath)
		if (err != nil || !info.IsDir()) && pack.Status != "deleted_by_user" {
			log.Printf("reconcileManagedPacks: Pack %v marked as deleted_by_user\n", pack.Name)
			pack.Status = "deleted_by_user"
			pack.Reason = fmt.Sprintf(
				"reconcileManagedPacks did not find %s",
				packPath,
			)
		}
		managedPacks[id] = pack
	}
}

func downloadWantedPacks(desiredPacks map[int]bool, managedPacks map[int]PackStatus, packFolder string, smPackSearchURL string) {
	attempts := 0
	failed := 0
	for packID, _ := range desiredPacks {
		// Check if the pack is not in the managedPacks list
		if _, exists := managedPacks[packID]; !exists {
			log.Printf("Pack id %d is in desired packs, not in the managed packs DB\n", packID)
			status := PackStatus{
				ID:     packID,
				Status: "pending_install",
			}

			// Download the pack
			attempts += 1
			packName, err := downloadPack(packID, smPackSearchURL, packFolder)
			if err != nil {
				log.Printf("downloadPack error: %v", err)
			}
			status.Status = "installed"
			status.Name = strings.TrimSuffix(packName, ".zip")
			if err != nil {
				status.Status = "download_failed"
				status.Reason = fmt.Sprintf("download failed: %v", err)
				failed += 1
			}
			managedPacks[status.ID] = status
		}
	}
	log.Printf("downloadWantedPacks: Finish checking for wanted packs. Checked %d - Downloaded %d - Failed: %d", len(desiredPacks), attempts-failed, failed)
}

func removeUnwantedPacks(packs map[int]bool, managedPacks map[int]PackStatus, packFolder string) {
	// Check all packs in statuses
	//  For any entry not in pack, but in statuses
	//      => Delete pack from the filesystem
	attempts := 0
	failed := 0
	for id, packDBEntry := range managedPacks {
		if !packs[id] {
			// Set to pending_delete
			packDBEntry.Status = "pending_delete"
			attempts += 1
			err := deletePack(managedPacks[id].Name, packFolder)
			if err != nil {
				packDBEntry.Status = "delete_failed"
				packDBEntry.Reason = fmt.Sprintf("delete failed: %v", err)
				failed += 1
			} else {
				// Remove entry from statuses slice
				delete(managedPacks, id)
			}
		}
	}
	log.Printf("removeUnwantedPacks: Finish checking for wanted packs. Checked %d - Removed %d - Failed: %d", len(packs), attempts-failed, failed)
}

// Read  the file packs.yaml
func loadPacks(path string) (map[int]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read packs file: %w", err)
	}

	// Parse packs file
	var packsFile PacksFile
	if err := yaml.Unmarshal(data, &packsFile); err != nil {
		return nil, fmt.Errorf("failed to parse packs file: %w", err)
	}

	// Create a hashmap with ids, so we can
	//	track which id exists
	packsMap := make(map[int]bool)
	for _, pack := range packsFile.Packs {
		// Ensure there is a valid ID field
		if pack.ID == 0 {
			log.Printf("loadPacks: Error parsing pack. Ensure correct ID %+v", pack)
		} else {
			packsMap[pack.ID] = true
		}
	}
	return packsMap, nil
}

func loadPackStatuses(path string) (map[int]PackStatus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read status file: %w", err)
	}

	var managedPacks []PackStatus
	if err := yaml.Unmarshal(data, &managedPacks); err != nil {
		return nil, fmt.Errorf("failed to parse status file: %w", err)
	}
	log.Printf("loadPackStatuses: Loaded %d entries", len(managedPacks))
	statusMap := make(map[int]PackStatus)
	for _, status := range managedPacks {
		statusMap[status.ID] = status
	}

	return statusMap, nil
}

func writePackStatusDB(managedPacks []PackStatus, path string) error {
	data, err := yaml.Marshal(managedPacks)
	if err != nil {
		return fmt.Errorf("failed to serialize managedPacks: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// downloadPack downloads a pack from the StepMania Online server
func downloadPack(id int, smSearchUrl string, targetDir string) (string, error) {
	url := fmt.Sprintf("%s/download/pack/%d", strings.TrimRight(smSearchUrl, "/"), id)
	log.Printf("Downloading pack id [%d] in [%v] from [%v]\n", id, targetDir, url)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download pack %d - HTTP %d\n", id, resp.StatusCode)
		return "", fmt.Errorf("download failed with status code: %d", resp.StatusCode)
	}

	// Get pack name from response headers
	contentDisposition := resp.Header.Get("Content-Disposition")
	var fileName string
	for _, part := range strings.Split(contentDisposition, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "filename=") {
			fileName = strings.TrimPrefix(part, "filename=")
			fileName = strings.Trim(fileName, "\"'")
			break
		}
	}

	// Error if no filename
	if fileName == "" {
		return "error-name", fmt.Errorf("downloadPack: Could not fetch pack name")
	}

	// Save the zip file to temp location first
	tempFile := filepath.Join(os.TempDir(), fileName)
	tmp, err := os.Create(tempFile)
	if err != nil {
		return fileName, err
	}
	defer func() {
		os.Remove(tempFile) // Clean up temp file after processing
	}()

	_, err = io.Copy(tmp, resp.Body)
	if err != nil {
		return fileName, err
	}
	tmp.Close()

	// Extract the zip to target directory
	err = os.MkdirAll(targetDir, 0755)
	if err != nil {
		return fileName, err
	}

	return fileName, extractZip(tempFile, targetDir)
}

func extractZip(zipPath string, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Skip directories (they will be created as needed)
		if strings.HasSuffix(f.Name, "/") {
			continue
		}

		// Determine path for file
		destPath := filepath.Join(destDir, f.Name)

		// Ensure directory exists
		dir := filepath.Dir(destPath)
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Open the file in the archive
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open %s from zip: %w", f.Name, err)
		}
		defer rc.Close()

		// Write file contents
		out, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", destPath, err)
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()

		if err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}
	}

	return nil
}

// deletePack deletes the pack directory from the pack folder
func deletePack(packName string, targetDir string) error {
	packPath := filepath.Join(targetDir, packName)

	// Check if the path exists and is a directory
	info, err := os.Stat(packPath)
	if err != nil {
		return fmt.Errorf("cannot access pack directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("pack path is not a directory")
	}

	// Remove the directory and all its contents
	return os.RemoveAll(packPath)
}

// Download into `desiredPackPath` the `packs.yaml` file hosted at the given URL
func downloadPackYaml(desiredPackPath string, url string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status downloading %s: %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var packsFile PacksFile
	if err := yaml.Unmarshal(body, &packsFile); err != nil {
		return fmt.Errorf("invalid yaml: %w", err)
	}

	if len(packsFile.Packs) == 0 {
		return fmt.Errorf("yaml is valid but has no entries under 'packs'")
	}

	for i, pack := range packsFile.Packs {
		if pack.ID == 0 {
			return fmt.Errorf("pack entry at index %d is missing a valid 'id'", i)
		}
	}

	if err := os.WriteFile(desiredPackPath, body, 0644); err != nil {
		return fmt.Errorf("failed to write file to %s: %w", desiredPackPath, err)
	}
	return nil
}
