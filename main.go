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

func execAttach(cmd ...string) error {
	r := exec.Command(cmd[0], cmd[1:]...)
	r.Stdin = os.Stdin
	r.Stdout = os.Stdout
	r.Stderr = os.Stderr
	return r.Run()
}

func startServer() error {
	logger.Info("starting server")
	return execAttach("/server/startserver.sh", "-configfile=/server/serverconfig.xml", "-logfile", "/dev/stderr")
}

func extract(src string, dest string) error {
	logger.Info("extract", "src", src, "dest", dest)

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

type downloadCallback func(string) error

func download(src string, cb downloadCallback) error {
	logger.Info("download", "src", src)
	td, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(td)

	filePath := path.Join(td, filepath.Base(src))
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	resp, err := http.Get(src)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	err = cb(filePath)
	if err != nil {
		return err
	}

	return nil
}

func createDirectories() error {
	logger.Info("create directories")

	paths := []string{
		"/server/Mods",
		"/data",
	}
	for _, path := range paths {
		err := os.Mkdir(path, 0)
		if os.IsExist(err) {
			stat, _ := os.Lstat(path)
			if !stat.IsDir() {
				return fmt.Errorf("path %s not a directory", path)
			}
			continue
		}
		logger.Info("create directory", "path", path)
	}
	return nil
}

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

type serverSettings struct {
	XMLName    xml.Name   `xml:"ServerSettings"`
	Properties []property `xml:"property"`
}

type property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

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
	err = os.WriteFile("/server/serverconfig.default.xml", defaultBytes, 0)
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
	return os.WriteFile("/server/serverconfig.xml", xmlBytes, 0)
}

func main() {
	handleError := func(err error) {
		if err == nil {
			return
		}
		logger.Error(err.Error())
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		handleError(execAttach(os.Args[1:]...))
		return
	}

	type step func() error
	steps := []step{
		createDirectories,
		installRootFiles,
		installModFiles,
		generateServerSettings,
		startServer,
	}

	for _, step := range steps {
		handleError(step())
	}
}
