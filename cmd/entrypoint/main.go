package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	osuser "os/user"
)

var (
	folderData           = "/data"
	folderGenerated      = "/generated"
	folderServer         = "/server"
	envDeleteDefaultMods = "DELETE_DEFAULT_MODS"
	envGid               = "GID"
	envModUrls           = "MOD_URLS"
	envRootUrls          = "ROOT_URLS"
	envSettingPrefix     = "SETTING_"
	envUid               = "UID"
	logger               = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	userGid              = "1000"
	userName             = "sdtd"
	userUid              = "1000"
)

// user is a struct containing the uid and gid of a local user
type user struct {
	uid int
	gid int
}

// Returns a command friendly version of a [user] in the form of <uid>:<gid>.
func (u user) String() string {
	return fmt.Sprintf("%d:%d", u.uid, u.gid)
}

// Reads from [net.Conn] until a pattern is found or a timeout occurs.
// Raises an error if the connection read fails.
// Raises an error if a timeout occurs
func readUntilPattern(conn net.Conn, pattern string, timeout time.Duration) error {
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

// dialServer connects to the running seven days to die server, waits for the server to accept commands, and then invokes the provided callback with the opened connection.
// Raises an error if the server is not connectable
// Raises an error if the server times out while waiting to accept commands
// Raises an error if the callback raises an error
func dialServer(cb dialServerCb) error {
	addr := "localhost:8081"

	logger.Info("dialing server", "addr", addr)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	pattern := "Press 'help' to get a list of all commands. Press 'exit' to end session."
	err = readUntilPattern(conn, pattern, 5*time.Second)
	if err != nil {
		return err
	}

	return cb(conn)
}

// signalHandler holds data associated with signal handling
// Should not be created directly, see [handleSignals]
type signalHandler struct {
	signal        *os.Signal
	signalChannel chan os.Signal
	signalHandled chan bool
}

// Unregisters a signal handler from the signaling system
func (sh *signalHandler) Stop() {
	signal.Stop(sh.signalChannel)
}

// Waits for the [signalHandlerCallback] to finish *only if* a signal was caught.  Otherwise, is a no-op.
func (sh *signalHandler) Wait() {
	if sh.signal == nil {
		return
	}
	logger.Info("waiting for signal handler")
	<-sh.signalHandled
}

// signalHandlerCallback is the callback invoked when a signal is intercepted
type signalHandlerCallback func(sig os.Signal)

// Creates a signal handler that listens to common signals (SIGINT, SIGTERM) and calls the provided [signalHandlerCallback].
func handleSignals(callback signalHandlerCallback) *signalHandler {
	sh := signalHandler{
		signal:        nil,
		signalChannel: make(chan os.Signal, 1),
		signalHandled: make(chan bool, 1),
	}

	signal.Notify(sh.signalChannel, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		signal := <-sh.signalChannel
		sh.signal = &signal
		logger.Info("caught signal", "signal", *sh.signal)
		callback(*sh.signal)
		sh.signalHandled <- true
	}()

	return &sh
}

// execOpts are used to customize [runCmd] behavior.
type execOpts struct {
	AsUser  user
	Attach  bool
	Cwd     string
	Env     []string
	Signals bool
}

// Helper method to run a command [cmdSlice] with the given options [execOpts].
// Returns an error if the command fails.
func runCmd(cmdSlice []string, opts execOpts) error {
	if opts.AsUser != (user{}) {
		currentUser, err := getCurrentUser()
		if err != nil {
			return err
		}
		if currentUser != opts.AsUser {
			cmdSlice = append([]string{"gosu", opts.AsUser.String()}, cmdSlice...)
		}
	}

	cmd := exec.Command(cmdSlice[0], cmdSlice[1:]...)
	var signalHandler *signalHandler
	if opts.Attach {
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
	}
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	if opts.Signals {
		signalHandler = handleSignals(func(sig os.Signal) {
			cmd.Process.Signal(sig)
		})
	}

	logger.Info("run cmd", "cmd", strings.Join(cmdSlice, " "))
	err := cmd.Run()

	if signalHandler != nil {
		signalHandler.Wait()
	}

	return err
}

// Gets the current user as a [user] object.
// Returns an error if the current user can't be determined.
// Returns an error if the current user's uid/gid are unparseable.
func getCurrentUser() (user, error) {
	fail := func(err error) (user, error) {
		return user{}, err
	}

	currentUser, err := osuser.Current()
	if err != nil {
		return fail(err)
	}
	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil {
		return fail(fmt.Errorf("invalid uid %s", currentUser.Uid))
	}
	gid, err := strconv.Atoi(currentUser.Gid)
	if err != nil {
		return fail(fmt.Errorf("invalid gid %s", currentUser.Gid))
	}

	return user{uid, gid}, nil
}

// Gets a [user] as defined by the UID/GID environment variables.
// Defaults to a uid of [userUid] if unset.
// Defaults to a gid of [userGid] if unset.
func getEnvUser() (user, error) {
	fail := func(err error) (user, error) {
		return user{}, err
	}

	uidString := os.Getenv(envUid)
	if uidString == "" {
		uidString = userUid
	}
	uid, err := strconv.Atoi(uidString)
	if err != nil {
		return fail(fmt.Errorf("invalid uid %s", uidString))
	}

	gidString := os.Getenv(envGid)
	if gidString == "" {
		gidString = userGid
	}
	gid, err := strconv.Atoi(gidString)
	if err != nil {
		return fail(fmt.Errorf("invalid gid %s", gidString))
	}

	return user{uid, gid}, nil
}

// Invokes a subprocess running the command stored in [args].  Attaches the parent stdin/stderr/stdout to the subprocess.
// Returns an error if a uid/gid cannot be obtained from the environment.
// Returns an error if the subprocess fails.
// See: [getEnvUser]
func passthrough(args ...string) error {
	logger.Info("passthrough")

	user, err := getEnvUser()
	if err != nil {
		return err
	}

	return runCmd(args, execOpts{AsUser: user, Attach: true, Signals: true})
}

// Extracts the file at [src] to the directory at [dest].  Creates [dest] if the path does not exist.
// Returns an error if [dest] is inaccessible.
// Returns an error if creating a non-existent [dest] fails.
// Returns an error if the various extraction commands fail.
func extract(src string, dest string) error {
	logger.Info("extract", "src", src, "dest", dest)

	_, err := os.Lstat(dest)
	if os.IsNotExist(err) {
		logger.Info("create directory", "path", dest)
		err = os.MkdirAll(dest, 0755)
	}
	if err != nil {
		return err
	}

	if strings.HasSuffix(src, ".zip") {
		return runCmd([]string{"unzip", "-o", src, "-d", dest}, execOpts{})
	} else if strings.HasSuffix(src, ".tar.gz") {
		return runCmd([]string{"tar", "--overwrite", "-xzf", src, "-C", dest}, execOpts{})
	} else if strings.HasSuffix(src, ".rar") {
		return runCmd([]string{"unrar", "-f", "-x", src, dest}, execOpts{})
	}
	return fmt.Errorf("unrecongized file type %s", src)
}

type downloadCb func(path string) error

// Downloads a file from [url] to a temporary file which is then passed to the callback [cb].
// Returns an error if the download fails.
// Returns an error if the callback returns an error.
func download(url string, cb downloadCb) error {
	tempDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	baseName := filepath.Base(url)
	tempFile := filepath.Join(tempDir, baseName)
	handle, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer handle.Close()

	logger.Info("download", "url", url, "file", tempFile)
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s sent non-200 status code: %d", url, response.StatusCode)
	}

	chunkSize := 1024 * 1024
	_, err = io.CopyBuffer(handle, response.Body, make([]byte, chunkSize))
	if err != nil {
		return err
	}

	return cb(tempFile)
}

// Downloads and extracts [urls] to [dest] folder.
// Returns an error if any part of the download or extraction fails.
func installUrls(dest string, urls ...string) error {
	for _, url := range urls {
		logger.Info("install url", "url", url, "dest", dest)
		err := download(url, func(src string) error {
			return extract(src, dest)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Converts a comma-separated environment variable into a [[]string].
func parseEnvList(varName string) []string {
	items := []string{}
	for _, item := range strings.Split(os.Getenv(varName), ",") {
		item = strings.Trim(item, " ")
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	return items
}

// Shuts down a seven days to die server by connecting to its telnet port and sending the 'shutdown' command.
// Raises an error if connecting to the server fails.
// Raises an error if the server fails to send the command.
func shutdownSdtdServer() error {
	return dialServer(func(conn net.Conn) error {
		_, err := conn.Write([]byte("shutdown\n"))
		return err
	})
}

// Starts the seven days to die server.
// Returns an error if the underlying command fails.
func startSdtdServer(config string) error {
	logger.Info("start sdtd server", "config", config)

	signalHandler := handleSignals(func(sig os.Signal) {
		shutdownSdtdServer()
	})
	defer signalHandler.Stop()

	env := append(os.Environ(), "LD_LIBRARY_PATH=.")
	cmd := []string{"./7DaysToDieServer.x86_64", "-batchmode", fmt.Sprintf("-configfile=%s", config), "-dedicated", "-logfile", "-nographics", "-quit"}
	err := runCmd(cmd, execOpts{Attach: true, Cwd: folderServer, Env: env})

	signalHandler.Wait()

	return err
}

// serverSettings is the root XML element stored in a seven days to die server settings XML file.
type serverSettings struct {
	XMLName    xml.Name   `xml:"ServerSettings"`
	Properties []property `xml:"property"`
}

// property is an item within a seven days to die server settings XML file.
type property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Parses server settings from the seven days to die server root directory.  Assumes the data is unmodified.
// Returns an error if the server settings configuration file is unreadable
// Returns an error if the server settings data is unparseable
func getDefaultServerSettings() (map[string]string, error) {
	fail := func(err error) (map[string]string, error) {
		return map[string]string{}, err
	}

	settingsBytes, err := os.ReadFile(filepath.Join(folderServer, "serverconfig.xml"))
	if err != nil {
		return fail(err)
	}
	settings := serverSettings{}
	err = xml.Unmarshal(settingsBytes, &settings)
	if err != nil {
		return fail(err)
	}

	data := map[string]string{}
	for _, property := range settings.Properties {
		data[property.Name] = property.Value
	}

	return data, err
}

// Parses server settings from the environment (identified as environment variables prefixed with SETTING_).
func getEnvServerSettings() map[string]string {
	data := map[string]string{}
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if !strings.HasPrefix(parts[0], envSettingPrefix) {
			continue
		}
		parts[0] = strings.TrimPrefix(parts[0], envSettingPrefix)
		data[parts[0]] = parts[1]
	}
	return data
}

// merges a list of maps (in order) - producing a final map.
func merge(items ...map[string]string) map[string]string {
	data := map[string]string{}
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
func writeServerSettings(data map[string]string, path string) error {
	logger.Info("write settings", "path", path)

	settings := serverSettings{}
	keys := []string{}
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, Name := range keys {
		Value := data[Name]
		logger.Info("setting", "name", Name, "value", Value)
		settings.Properties = append(settings.Properties, property{Name, Value})
	}

	dataBytes, err := xml.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, []byte(xml.Header+"\n"+string(dataBytes)), 0755)
}

// Clears a directory of its contents by deleting the folder and recreating it
func clearDirectory(path string) error {
	_, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err == nil {
		logger.Info("remove directory", "path", path)
		err = os.RemoveAll(path)
		if err != nil {
			return err
		}
	}

	logger.Info("create directory", "path", path)
	err = os.MkdirAll(path, 0755)
	if err != nil {
		return err
	}

	return nil
}

// Performs initial setup and the launches the seven days to die server.
// Assumes that the local runtime environment has been bootstrapped.
// Returns an error if any part of the process fails.
// See: [bootstrap].
func entrypoint() error {
	modFolder := filepath.Join(folderServer, "Mods")
	if os.Getenv(envDeleteDefaultMods) == "1" {
		err := clearDirectory(modFolder)
		if err != nil {
			return err
		}
	}

	rootUrls := parseEnvList(envRootUrls)
	err := installUrls(folderServer, rootUrls...)
	if err != nil {
		return err
	}

	modUrls := parseEnvList(envModUrls)
	err = installUrls(modFolder, modUrls...)
	if err != nil {
		return err
	}

	settingsFile := filepath.Join(folderGenerated, "serverconfig.xml")
	settings, err := getDefaultServerSettings()
	if err != nil {
		return err
	}
	settings = merge(
		settings,
		map[string]string{
			"WebDashboardEnabled": "true", // by default, enable web dashboard
		},
		getEnvServerSettings(),
		map[string]string{
			"TelnetEnabled":    "true",     // force telnet to be enabled (for graceful shutdown and health checks)
			"TelnetPort":       "8081",     // force telnet port to match exposed docker port
			"UserDataFolder":   folderData, // force user data folder to be located at [folderData]
			"WebDashboardPort": "8080",     // force web dashboard port to match exposed docker port
		},
	)
	err = writeServerSettings(settings, settingsFile)
	if err != nil {
		return err
	}

	return startSdtdServer(settingsFile)
}

// Checks the health of the seven days to die server by attempting to connect to the server's telnet port.
// If the connection fails, returns an error
func checkHealth() error {
	healthy := false
	err := dialServer(func(conn net.Conn) error {
		healthy = true
		return nil
	})
	logger.Info("health check", "healthy", healthy)
	return err
}

// Updates the non-root user to assume the uid/gid of [toUser].
// Returns an error if [toUser] is the root user.
// Returns an error if the non-root user cannot be found.
// Returns an error if setting either the uid/gid of the non-root user fails.
func updateNonRootUser(toUser user) error {
	if toUser.uid == 0 || toUser.gid == 0 {
		return fmt.Errorf("refusing to set non-root uid/gid to 0")
	}

	user, err := osuser.Lookup(userName)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(user.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(user.Gid)
	if err != nil {
		return err
	}

	if uid != toUser.uid {
		logger.Info("change uid", "user", userName, "from", uid, "to", toUser.uid)
		err := runCmd([]string{"usermod", "-u", strconv.Itoa(toUser.uid), userName}, execOpts{})
		if err != nil {
			return err
		}
	}

	if gid != toUser.gid {
		logger.Info("change gid", "user", userName, "from", gid, "to", toUser.gid)
		err := runCmd([]string{"groupmod", "-g", strconv.Itoa(toUser.gid), userName}, execOpts{})
		if err != nil {
			return err
		}
	}

	return nil
}

// Creates and sets [user] as the owner of [directories].
// Returns an error if an existing directory is inaccessible
// Returns an error if a non-existing directory cannot be created
// Returns an error if attempts to take ownership of an existing directory fails.
func setDirectoryOwner(user user, directories ...string) error {
	for _, directory := range directories {
		stat, err := os.Lstat(directory)
		if os.IsNotExist(err) {
			logger.Info("create directory", "path", directory)
			err = os.MkdirAll(directory, 0755)
			if err != nil {
				return err
			}
			stat, err = os.Lstat(directory)
		}
		if err != nil {
			return err
		}
		if !stat.IsDir() {
			return fmt.Errorf("path %s not a directory", directory)
		}

		logger.Info("set directory owner", "path", directory, "uid", user.uid, "gid", user.gid)
		err = runCmd([]string{"chown", "-R", user.String(), directory}, execOpts{})
		if err != nil {
			return err
		}
	}
	return nil
}

// Relaunches this entrypoint, but as the desired user and with the _BOOTSTRAPPED environment variable set.
// Returns an error if the current executable cannot be obtained.
// Returns an error if the relaunched entrypoint fails.
// See: [main]
func relaunchEntrypoint(user user) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := []string{executable}
	env := append(os.Environ(), "_BOOTSTRAPPED=1")
	return runCmd(cmd, execOpts{AsUser: user, Attach: true, Env: env, Signals: true})
}

// Bootstraps the runtime environment.
// If the current user is root:
//   - Will ensure non-root user is UID/GID (if UID/GID is non-root)
//   - Will ensure non-root owns critical directories
//   - Will relaunch entrypoint with UID/GID
//
// If the current user is non-root:
//   - Will relaunch entrypoint with current uid/gid
//
// Returns an error if the current user can't be determined
// Returns an error if the a user from can't be determined from the environment.
// Returns an error if updating the non-root user fails.
// Returns an error if changing directory ownership fails.
// Returns an error if relaunching the entrypoint fails.
func bootstrap() error {
	currentUser, err := getCurrentUser()
	if err != nil {
		return err
	}
	desiredUser := currentUser

	if currentUser.uid == 0 {
		envUser, err := getEnvUser()
		if err != nil {
			return err
		}
		desiredUser = envUser

		if envUser.uid != 0 {
			err := updateNonRootUser(envUser)
			if err != nil {
				return err
			}
		}

		err = setDirectoryOwner(envUser, folderData, folderGenerated, folderServer)
		if err != nil {
			return err
		}
	}

	return relaunchEntrypoint(desiredUser)
}

// The main function for the entrypoint.
func main() {
	var err error
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "health":
			err = checkHealth()
		default:
			err = passthrough(os.Args[1:]...)
		}
	} else if os.Getenv("_BOOTSTRAPPED") == "1" {
		err = entrypoint()
	} else {
		err = bootstrap()
	}

	code := 0
	if err != nil {
		logger.Error("error while running entrypoint", "msg", err.Error())
		code = 1
	}

	os.Exit(code)
}
