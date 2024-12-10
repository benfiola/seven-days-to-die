package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"encoding/xml"
)

var logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

// Atttach the current stdout/stderr/stdin to a given [exec.Cmd]
func attach(cmd *exec.Cmd) {
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
}

// Get uid/gid.
// Return error if UID/GID is unset.
func getUser() (string, string, error) {
	uid := os.Getenv("UID")
	gid := os.Getenv("GID")
	if uid == "" {
		return "", "", fmt.Errorf("UID unset")
	}
	if gid == "" {
		return "", "", fmt.Errorf("GID unset")
	}
	return uid, gid, nil
}

// Returns a [exec.Cmd] prepended with the `gosu` command.
func gosu(args ...string) (*exec.Cmd, error) {
	uid, gid, err := getUser()
	if err != nil {
		return nil, err
	}
	cmdSlice := append([]string{"gosu", fmt.Sprintf("%s:%s", uid, gid)}, args...)
	return exec.Command(cmdSlice[0], cmdSlice[1:]...), nil
}

// Runs the command provided to the entrypoint as a way to pass-through the default behavior
func passthrough(args ...string) error {
	// cmd, err := gosu(args...)
	cmd := exec.Command(args[0], args[1:]...)
	// if err != nil {
	// 	return err
	// }
	attach(cmd)
	return cmd.Run()
}

// Starts the SDTD server
func startServer() error {
	logger.Info("starting server")
	cmd, err := gosu("./7DaysToDieServer.x86_64", "-configfile=./serverconfig.xml", "-logfile", "-quit", "-batchmode", "-nographics", "-dedicated")
	if err != nil {
		return err
	}
	attach(cmd)
	cmd.Dir = "/server"
	return cmd.Run()
}

// Extracts a file located at src to the dest directory.
func extract(src string, dest string) error {
	logger.Info("extract", "src", src, "dest", dest)

	err := os.Mkdir(dest, 0755)
	if os.IsExist(err) {
		stat, _ := os.Lstat(dest)
		if !stat.IsDir() {
			return fmt.Errorf("path %s not a directory", dest)
		}
	} else {
		logger.Info("create directory", "path", dest)
	}

	var r *exec.Cmd
	if strings.HasSuffix(src, ".tar.gz") {
		r = exec.Command("tar", "xzf", src, "-C", dest)
	} else if strings.HasSuffix(src, ".zip") {
		r = exec.Command("unzip", src, "-d", dest)
	} else if strings.HasSuffix(src, ".rar") {
		r = exec.Command("unrar", "-x", src, dest)
	} else {
		return fmt.Errorf("unrecognized file type %s", src)
	}
	output, err := r.CombinedOutput()
	if err != nil {
		logger.Warn("extract failed", "cmd", strings.Join(r.Args, " "), "output", string(output))
	}
	return err
}

// downloadCallback is called with the temporary path that a file was downloaded to
type downloadCallback func(string) error

// Downloads src to a temporary path, then invokes the downloadCallback with the temporary path.  Performs cleanup when leaving function.
func download(src string, cb downloadCallback) error {
	logger.Info("download", "src", src)

	// create temp dir
	td, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(td)

	// create temp file (and open it)
	filePath := path.Join(td, filepath.Base(src))
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// download to temp file
	resp, err := http.Get(src)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	// invoke callback with temp file
	err = cb(filePath)
	if err != nil {
		return err
	}

	return nil
}

// Ensures base directories exist and have correct ownership
func ensureDirectories() error {
	logger.Info("ensure directories")

	uid, gid, err := getUser()
	if err != nil {
		return err
	}

	paths := []string{
		"/server",
		"/data",
	}
	for _, path := range paths {
		err := os.Mkdir(path, 0755)
		if os.IsExist(err) {
			stat, _ := os.Lstat(path)
			if !stat.IsDir() {
				return fmt.Errorf("path %s not a directory", path)
			}
		} else {
			logger.Info("create directory", "path", path)
		}

		logger.Info("set ownership", "path", path)
		cmd := exec.Command("chown", "-R", fmt.Sprintf("%s:%s", uid, gid), path)
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

// Installs root files (e.g., archives extracted to the sdtd root folder)
func installRootFiles() error {
	logger.Info("install root files")
	rootFiles := strings.Split(os.Getenv("ROOT_FILES"), ",")
	for _, rootFile := range rootFiles {
		rootFile = strings.Trim(rootFile, " ")
		if rootFile == "" {
			continue
		}
		err := download(rootFile, func(path string) error {
			return extract(path, "/server")
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Installs mod files (e.g., archives extracted to the sdtd Mods folder)
func installModFiles() error {
	logger.Info("install mod files")
	modFIles := strings.Split(os.Getenv("MOD_FILES"), ",")
	for _, modFile := range modFIles {
		modFile = strings.Trim(modFile, " ")
		if modFile == "" {
			continue
		}
		err := download(modFile, func(path string) error {
			return extract(path, "/server/Mods")
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// serverSettings is the root element for a SDTD Server Settings XML file
type serverSettings struct {
	XMLName    xml.Name   `xml:"ServerSettings"`
	Properties []property `xml:"property"`
}

// property defines a single property within a SDTD Server Settings XML file
type property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Processes the environment, producing a new SDTD Server Settings XML file overlaid on the existing, default server settings file.
func generateServerSettings() error {
	logger.Info("generate server settings")

	// build server settings from defaults
	defaultBytes, err := os.ReadFile("/server/serverconfig.xml")
	if err != nil {
		return err
	}
	defaults := serverSettings{}
	err = xml.Unmarshal(defaultBytes, &defaults)
	if err != nil {
		return err
	}
	settings := map[string]string{}
	for _, prop := range defaults.Properties {
		settings[prop.Name] = prop.Value
	}

	// backup default server settings
	err = os.WriteFile("/server/serverconfig.default.xml", defaultBytes, 0644)
	if err != nil {
		return err
	}

	// add environment overrides
	prefix := "SDTD_"
	for _, envString := range os.Environ() {
		envSlice := strings.SplitN(envString, "=", 2)
		if !strings.HasPrefix(envSlice[0], prefix) {
			continue
		}
		setting := strings.TrimPrefix(envSlice[0], prefix)
		settings[setting] = envSlice[1]
	}

	// add hardcoded overrides
	settings["UserDataFolder"] = "/data"

	// assemble settings
	serverSettings := serverSettings{}
	for k, v := range settings {
		logger.Info("setting", "key", k, "value", v)
		serverSettings.Properties = append(serverSettings.Properties, property{Name: k, Value: v})
	}

	// write xml
	xmlBytes, err := xml.MarshalIndent(serverSettings, "", "  ")
	if err != nil {
		return err
	}
	xmlBytes = []byte(xml.Header + string(xmlBytes))
	return os.WriteFile("/server/serverconfig.xml", xmlBytes, 0644)
}

// Runs the entrypoint
func main() {
	handleError := func(err error) {
		if err == nil {
			return
		}
		logger.Error(err.Error())
		os.Exit(1)
	}

	// if command provided to entrypoint, execute it.
	if len(os.Args) > 1 {
		handleError(passthrough(os.Args[1:]...))
		return
	}

	// perform default entrypoint behavior
	type step func() error
	steps := []step{
		installRootFiles,
		installModFiles,
		generateServerSettings,
		ensureDirectories,
		startServer,
	}
	for _, step := range steps {
		handleError(step())
	}
}
