package main

import (
	"context"
	_ "embed"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benfiola/game-server-helper/pkg/helper"
	"github.com/benfiola/game-server-helper/pkg/helperapi"
)

// Api wraps [helperapi.Api] and adds sdtd specific methods to the struct
type Api struct {
	helper.Api
}

// Defines a callback that accepts a [context.Context] and an [Api]
type Callback func(ctx context.Context, api Api) error

// Converts a [Callback] into an [helper.Callback] for compatibility with [helper.Helper]
func RunCallback(cb Callback) helper.Callback {
	return func(ctx context.Context, parent helper.Api) error {
		api := Api{Api: parent}
		return cb(ctx, api)
	}
}

// Conn wraps [net.Conn] and provides helper methods
type Conn struct {
	net.Conn
}

// Reads from [Conn] until a pattern is found or a timeout occurs.
// Raises an error if the connection read fails.
// Raises an error if a timeout occurs
func (conn Conn) ReadUntilPattern(pattern string, timeout time.Duration) error {
	start := time.Now()
	data := ""
	buf := make([]byte, 128)
	for {
		now := time.Now()
		if now.Sub(start) >= timeout {
			return fmt.Errorf("timed out reading until pattern")
		}
		read, err := conn.Read(buf)
		if err != nil {
			return nil
		}
		data += string(buf[:read])
		if strings.Contains(data, pattern) {
			break
		}
	}
	return nil
}

// dialServerCb is a callback provided to [dialServer] - allowing callers to futher operate on a connection to the server
type dialServerCb func(conn net.Conn) error

// DialServer connects to the running seven days to die server, waits for the server to accept commands, and then invokes the provided callback with the opened connection.
// Raises an error if the server is not connectable
// Raises an error if the server times out while waiting to accept commands
// Raises an error if the callback raises an error
func (api *Api) DialServer(cb dialServerCb) error {
	addr := "localhost:8081"
	api.Logger.Info("dialing server", "addr", addr)
	nconn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	conn := Conn{Conn: nconn}
	defer conn.Close()
	pattern := "Press 'help' to get a list of all commands. Press 'exit' to end session."
	err = conn.ReadUntilPattern(pattern, 5*time.Second)
	if err != nil {
		return err
	}
	return cb(conn)
}

// Shuts down a seven days to die server by connecting to its telnet port and sending the 'shutdown' command.
// Raises an error if connecting to the server fails.
// Raises an error if the server fails to send the command.
func (api *Api) ShutdownServer() error {
	api.Logger.Info("shutdown server")
	return api.DialServer(func(conn net.Conn) error {
		_, err := conn.Write([]byte("shutdown\n"))
		return err
	})
}

// Starts the seven days to die server.
// Returns an error if the underlying command fails.
func (api *Api) StartServer(config string) error {
	api.Logger.Info("start server", "config", config)
	cmdFinished := make(chan bool, 1)
	unregister := api.HandleSignal(func(sig os.Signal) {
		api.ShutdownServer()
		<-cmdFinished
	})
	defer unregister()
	env := append(os.Environ(), "LD_LIBRARY_PATH=.")
	cmd := []string{"./7DaysToDieServer.x86_64", "-batchmode", fmt.Sprintf("-configfile=%s", config), "-dedicated", "-logfile", "-nographics", "-quit"}
	_, err := api.RunCommand(cmd, helperapi.CmdOpts{Attach: true, Cwd: api.Directories["sdtd"], Env: env})
	cmdFinished <- true
	return err
}

// Wraps map[string]string and is capable of assembling an xml payload
type ServerSettings map[string]string

// Converts the [ServerSettings] data into an XML payload
func (ss *ServerSettings) Xml() XmlServerSettings {
	xss := XmlServerSettings{Properties: []XmlServerProperty{}}
	for name, value := range *ss {
		xsp := XmlServerProperty{Name: name, Value: value}
		xss.Properties = append(xss.Properties, xsp)
	}
	return xss
}

// XmlServerSettings is the xml representation of sdtd server settings
type XmlServerSettings struct {
	XMLName    xml.Name            `xml:"ServerSettings"`
	Properties []XmlServerProperty `xml:"property"`
}

// XmlServerProperty is an item within a seven days to die server settings XML file.
type XmlServerProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Converts the [XmlServerSettings] data into a typical golang map
func (xss *XmlServerSettings) Map() ServerSettings {
	data := map[string]string{}
	for _, p := range xss.Properties {
		data[p.Name] = p.Value
	}
	return data
}

// Parses server settings from the seven days to die server root directory.  Assumes the data is unmodified.
// Returns an error if the server settings configuration file is unreadable
// Returns an error if the server settings data is unparseable
func (api *Api) GetDefaultServerSettings() (ServerSettings, error) {
	file := filepath.Join(api.Directories["sdtd"], "serverconfig.xml")
	api.Logger.Info("get default server settings", "path", file)
	xss := XmlServerSettings{}
	err := api.UnmarshalFile(file, &xss)
	if err != nil {
		return nil, err
	}
	return xss.Map(), nil
}

// Parses server settings from the environment (identified as environment variables prefixed with SETTING_).
func (api *Api) GetEnvServerSettings() ServerSettings {
	data := ServerSettings{}
	prefix := "SETTING_"
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if !strings.HasPrefix(parts[0], prefix) {
			continue
		}
		parts[0] = strings.TrimPrefix(parts[0], prefix)
		data[parts[0]] = parts[1]
	}
	api.Logger.Info("get env server settings", "count", len(data))
	return data
}

// merges a list of [ServerSettings] (in order) - producing a final [ServerSettings].
func (api *Api) MergeServerSettings(items ...ServerSettings) ServerSettings {
	api.Logger.Info("merge server settings", "count", len(items))
	data := ServerSettings{}
	for _, item := range items {
		for k, v := range item {
			data[k] = v
		}
	}
	return data
}

// Writes server settings (presented as a map) as a server settings XML file stored at [path]
// Returns an error if the data cannot be serialized into XML
// Returns an error if the data cannot be written to [path]
func (api *Api) WriteServerSettings(settings ServerSettings) (string, error) {
	path := filepath.Join(api.Directories["generated"], "serverconfig.xml")
	api.Logger.Info("write server settings", "path", path)
	xmlServerSettings := settings.Xml()
	err := api.MarshalFile(xmlServerSettings, path)
	if err != nil {
		return "", err
	}
	return path, nil
}

// Downloads and extracts a list of mod urls to the given path.
// Returns an error if the download fails.
// Returns an error if the extraction fails.
func (api *Api) InstallMods(path string, mods ...string) error {
	for _, mod := range mods {
		api.Logger.Info("install mod", "path", path, "mod", mod)
		err := api.Download(mod, func(downloadPath string) error {
			return api.Extract(downloadPath, path)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Deletes default mods located in the sdtd 'Mods' folder.  This is done by deleting the folder and recreating it.
// Returns an error if the initial folder deletion fails.
// Returns an error if the subsequent folder creation fails.
func (api *Api) DeleteDefaultMods() error {
	api.Logger.Info("delete default mods")
	path := filepath.Join(api.Directories["sdtd"], "Mods")
	subpaths, err := api.ListDir(path)
	if err != nil {
		return err
	}
	err = api.RemovePaths(subpaths...)
	if err != nil {
		return err
	}
	return nil
}

// Downloads sdtd with DepotDownloader
func (api *Api) DownloadSdtd(manifestId string) error {
	cachePath := filepath.Join(api.Directories["cache"], manifestId)
	_, err := os.Lstat(cachePath)
	exists := true
	if errors.Is(err, os.ErrNotExist) {
		exists = false
		err = nil
	}
	if err != nil {
		return err
	}
	if !exists {
		api.Logger.Info("clear cache")
		paths, err := api.ListDir(api.Directories["cache"])
		if err != nil {
			return err
		}
		err = api.RemovePaths(paths...)
		if err != nil {
			return err
		}
		cacheTmp := filepath.Join(api.Directories["cache"], ".tmp")
		api.Logger.Info("download sdtd to cache", "manifest", manifestId)
		err = api.DepotDownload("294420", "294422", manifestId, cacheTmp, helperapi.DepotDownloadOpts{})
		if err != nil {
			return err
		}
		_, err = api.RunCommand([]string{"mv", cacheTmp, cachePath}, helperapi.CmdOpts{})
		if err != nil {
			return err
		}
	}
	api.Logger.Info("copy sdtd from cache")
	_, err = api.RunCommand([]string{"cp", "-R", fmt.Sprintf("%s/.", cachePath), api.Directories["sdtd"]}, helperapi.CmdOpts{})
	if err != nil {
		return err
	}
	api.Logger.Info("set server binary executable")
	serverBin := filepath.Join(api.Directories["sdtd"], "7DaysToDieServer.x86_64")
	err = os.Chmod(serverBin, 0755)
	if err != nil {
		return err
	}
	return nil
}

// EntrypointConfig is the configuration for the
type EntrypointConfig struct {
	DeleteDefaultMods bool     `env:"DELETE_DEFAULT_MODS"`
	ManifestId        string   `env:"MANIFEST_ID"`
	ModUrls           []string `env:"MOD_URLS"`
	RootUrls          []string `env:"ROOT_URLS"`
}

// Performs initial setup and the launches the seven days to die server.
// Assumes that the local runtime environment has been bootstrapped.
// Returns an error if any part of the process fails.
func Entrypoint(ctx context.Context, api Api) error {
	api.Logger.Info("entrypoint")

	config := EntrypointConfig{}
	err := api.ParseEnv(&config)
	if err != nil {
		return err
	}

	err = api.DownloadSdtd(config.ManifestId)
	if err != nil {
		return err
	}

	if config.DeleteDefaultMods {
		err := api.DeleteDefaultMods()
		if err != nil {
			return err
		}
	}

	err = api.InstallMods(api.Directories["sdtd"], config.RootUrls...)
	if err != nil {
		return err
	}

	err = api.InstallMods(filepath.Join(api.Directories["sdtd"], "Mods"), config.ModUrls...)
	if err != nil {
		return err
	}

	defaultSettings, err := api.GetDefaultServerSettings()
	if err != nil {
		return err
	}
	settingsFile, err := api.WriteServerSettings(api.MergeServerSettings(
		defaultSettings,
		ServerSettings{
			"WebDashboardEnabled": "true",
		},
		api.GetEnvServerSettings(),
		ServerSettings{
			"TelnetEnabled":    "true",                  // force telnet to be enabled (for graceful shutdown and health checks)
			"TelnetPort":       "8081",                  // force telnet port to match exposed docker port
			"UserDataFolder":   api.Directories["data"], // force user data folder to be located at [folderData]
			"WebDashboardPort": "8080",                  // force web dashboard port to match exposed docker port
		},
	))
	if err != nil {
		return err
	}

	return api.StartServer(settingsFile)
}

// Checks the health of the seven days to die server by attempting to connect to the server's telnet port.
// If the connection fails, returns an error
func HealthCheck(ctx context.Context, api Api) error {
	healthy := false
	err := api.DialServer(func(conn net.Conn) error {
		healthy = true
		return nil
	})
	api.Logger.Info("health check", "healthy", healthy)
	return err
}

//go:embed version.txt
var Version string

// The main function for the entrypoint.
func main() {
	(&helper.Helper{
		Directories: map[string]string{
			"cache":     "/cache",
			"data":      "/data",
			"generated": "/generated",
			"sdtd":      "/sdtd",
		},
		Entrypoint:  RunCallback(Entrypoint),
		HealthCheck: RunCallback(HealthCheck),
		Version:     Version,
	}).Run()
}
