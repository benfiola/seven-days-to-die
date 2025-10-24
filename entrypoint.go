package main

import (
	"context"
	_ "embed"
	"encoding/xml"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	helper "github.com/benfiola/game-server-helper/pkg"
)

// Conn wraps [net.Conn] and provides helper methods
type Conn struct {
	netConn net.Conn
	ctx     context.Context
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
		read, err := conn.netConn.Read(buf)
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
type dialServerCb func(conn Conn) error

// DialServer connects to the running seven days to die server, waits for the server to accept commands, and then invokes the provided callback with the opened connection.
// Raises an error if the server is not connectable
// Raises an error if the server times out while waiting to accept commands
// Raises an error if the callback raises an error
func DialServer(ctx context.Context, cb dialServerCb) error {
	addr := "localhost:8081"
	helper.Logger(ctx).Info("dialing server", "addr", addr)
	nconn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	conn := Conn{ctx: ctx, netConn: nconn}
	defer conn.netConn.Close()
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
func ShutdownServer(ctx context.Context) error {
	helper.Logger(ctx).Info("shutdown server")
	return DialServer(ctx, func(conn Conn) error {
		_, err := conn.netConn.Write([]byte("shutdown\n"))
		return err
	})
}

// Starts the seven days to die server.
// Returns an error if the underlying command fails.
func StartServer(ctx context.Context, config string) error {
	helper.Logger(ctx).Info("start server", "config", config)
	cmdFinished := make(chan bool, 1)
	unregister := helper.HandleSignal(ctx, func(sig os.Signal) {
		ShutdownServer(ctx)
		<-cmdFinished
	})
	defer unregister()
	env := append(os.Environ(), "LD_LIBRARY_PATH=.")
	cmd := []string{"./7DaysToDieServer.x86_64", "-batchmode", fmt.Sprintf("-configfile=%s", config), "-dedicated", "-logfile", "-nographics", "-quit"}
	_, err := helper.Command(ctx, cmd, helper.CmdOpts{Attach: true, Cwd: helper.Dirs(ctx)["sdtd"], Env: env, IgnoreSignals: true}).Run()
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
func GetDefaultServerSettings(ctx context.Context) (ServerSettings, error) {
	fail := func(err error) (ServerSettings, error) {
		return nil, err
	}
	file := filepath.Join(helper.Dirs(ctx)["sdtd"], "serverconfig.xml")
	helper.Logger(ctx).Info("get default server settings", "path", file)
	xss := XmlServerSettings{}
	err := helper.UnmarshalFile(ctx, file, &xss)
	if err != nil {
		return fail(err)
	}
	return xss.Map(), nil
}

// Parses server settings from the environment (identified as environment variables prefixed with SETTING_).
func GetEnvServerSettings(ctx context.Context) ServerSettings {
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
	helper.Logger(ctx).Info("get env server settings", "count", len(data))
	return data
}

// Merges a list of [ServerSettings] (in order) - producing a final [ServerSettings].
func MergeServerSettings(items ...ServerSettings) ServerSettings {
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
func WriteServerSettings(ctx context.Context, settings ServerSettings) (string, error) {
	fail := func(err error) (string, error) {
		return "", err
	}
	path := filepath.Join(helper.Dirs(ctx)["generated"], "serverconfig.xml")
	helper.Logger(ctx).Info("write server settings", "path", path)
	xmlServerSettings := settings.Xml()
	err := helper.MarshalFile(ctx, xmlServerSettings, path)
	if err != nil {
		return fail(err)
	}
	return path, nil
}

// Downloads and extracts a list of mod urls to the given path.
// Returns an error if the download fails.
// Returns an error if the extraction fails.
func InstallMods(ctx context.Context, path string, mods ...string) error {
	for _, mod := range mods {
		helper.Logger(ctx).Info("install mod", "path", path, "mod", mod)
		key := fmt.Sprintf("mod-%s", filepath.Base(mod))
		err := helper.CacheFile(ctx, key, path, func(dest string) error {
			return helper.CreateTempDir(ctx, func(tempDir string) error {
				downloadPath := filepath.Join(tempDir, filepath.Base(mod))
				err := helper.Download(ctx, mod, downloadPath)
				if err != nil {
					return err
				}
				return helper.Extract(ctx, downloadPath, dest)
			})
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
func DeleteDefaultMods(ctx context.Context) error {
	helper.Logger(ctx).Info("delete default mods")
	path := filepath.Join(helper.Dirs(ctx)["sdtd"], "Mods")
	subpaths, err := helper.ListDir(ctx, path)
	if err != nil {
		return err
	}
	return helper.RemovePaths(ctx, subpaths...)
}

// Downloads sdtd with DepotDownloader
func DownloadSdtd(ctx context.Context, manifestId string) error {
	key := fmt.Sprintf("sdtd-%s", manifestId)
	err := helper.CacheFile(ctx, key, helper.Dirs(ctx)["sdtd"], func(dest string) error {
		helper.Logger(ctx).Info("download sdtd", "manifest", manifestId)
		_, err := helper.Command(ctx, []string{"DepotDownloader", "-app", "294420", "-depot", "294422", "-manifest", manifestId, "-dir", dest}, helper.CmdOpts{}).Run()
		return err
	})
	if err != nil {
		return err
	}
	helper.Logger(ctx).Info("set server binary executable")
	serverBin := filepath.Join(helper.Dirs(ctx)["sdtd"], "7DaysToDieServer.x86_64")
	return os.Chmod(serverBin, 0755)
}

// EntrypointConfig is the configuration for the
type EntrypointConfig struct {
	DeleteDefaultMods  bool           `env:"DELETE_DEFAULT_MODS"`
	ManifestId         string         `env:"MANIFEST_ID"`
	ModUrls            []string       `env:"MOD_URLS"`
	RootUrls           []string       `env:"ROOT_URLS"`
	AutoRestart        *time.Duration `env:"AUTO_RESTART"`
	AutoRestartMessage string         `env:"AUTO_RESTART_MESSAGE" envDefault:"Restarting server in 1 minute"`
}

// Performs initial setup and the launches the seven days to die server.
// Assumes that the local runtime environment has been bootstrapped.
// Returns an error if any part of the process fails.
func Entrypoint(ctx context.Context) error {
	helper.Logger(ctx).Info("entrypoint")

	config := EntrypointConfig{}
	err := helper.ParseEnv(ctx, &config)
	if err != nil {
		return err
	}

	err = DownloadSdtd(ctx, config.ManifestId)
	if err != nil {
		return err
	}

	if config.DeleteDefaultMods {
		err := DeleteDefaultMods(ctx)
		if err != nil {
			return err
		}
	}

	err = InstallMods(ctx, helper.Dirs(ctx)["sdtd"], config.RootUrls...)
	if err != nil {
		return err
	}

	err = InstallMods(ctx, filepath.Join(helper.Dirs(ctx)["sdtd"], "Mods"), config.ModUrls...)
	if err != nil {
		return err
	}

	defaultSettings, err := GetDefaultServerSettings(ctx)
	if err != nil {
		return err
	}
	settingsFile, err := WriteServerSettings(ctx, MergeServerSettings(
		defaultSettings,
		ServerSettings{
			"WebDashboardEnabled": "true",
		},
		GetEnvServerSettings(ctx),
		ServerSettings{
			"TelnetEnabled":    "true",                   // force telnet to be enabled (for graceful shutdown and health checks)
			"TelnetPort":       "8081",                   // force telnet port to match exposed docker port
			"UserDataFolder":   helper.Dirs(ctx)["data"], // force user data folder to be located at [folderData]
			"WebDashboardPort": "8080",                   // force web dashboard port to match exposed docker port
		},
	))
	if err != nil {
		return err
	}
	if config.AutoRestart != nil {
		go func() {
			time.Sleep(*config.AutoRestart - time.Minute)
			DialServer(ctx, func(conn Conn) error {
				_, err := conn.netConn.Write([]byte(fmt.Sprintf("say \"%s\"\n", config.AutoRestartMessage)))
				return err
			})
			time.Sleep(time.Minute)
			ShutdownServer(ctx)
		}()
	}
	return StartServer(ctx, settingsFile)
}

// Checks the health of the seven days to die server by attempting to connect to the server's telnet port.
// If the connection fails, returns an error
func CheckHealth(ctx context.Context) error {
	healthy := false
	err := DialServer(ctx, func(conn Conn) error {
		healthy = true
		return nil
	})
	helper.Logger(ctx).Info("health check", "healthy", healthy)
	return err
}

//go:embed version.txt
var Version string

// The main function for the entrypoint.
func main() {
	wd, _ := os.Getwd()
	(&helper.Entrypoint{
		Dirs: map[string]string{
			"cache":     filepath.Join(wd, "cache"),
			"data":      filepath.Join(wd, "data"),
			"generated": filepath.Join(wd, "generated"),
			"sdtd":      filepath.Join(wd, "sdtd"),
		},
		CheckHealth: CheckHealth,
		Main:        Entrypoint,
		Version:     Version,
	}).Run()
}
