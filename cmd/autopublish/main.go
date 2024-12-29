package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/andygrunwald/vdf"
	"github.com/google/go-github/v68/github"
)

var (
	envGithubToken         = "GITHUB_TOKEN"
	envSteamUsername       = "STEAM_USERNAME"
	envSteamPassword       = "STEAM_PASSWORD"
	githubOwner            = "benfiola"
	githubRepo             = "seven-days-to-die"
	githubWorkflowFilename = "publish.yaml"
	logger                 = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	steamAppId             = "294420"
	steamDepotId           = "294422"
	steamBranchName        = "latest_experimental"
)

// Runs the given [command] and returns its stdout.
// Raises an error if the command fails
func runCmd(command []string) (string, error) {
	cmd := exec.Command(command[0], command[1:]...)
	stdoutBuffer := strings.Builder{}
	stderrBuffer := strings.Builder{}
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err := cmd.Run()
	return stdoutBuffer.String(), err
}

// steamCredentials are a collection of username and password used to authenticate with Steam.
type steamCredentials struct {
	Username string
	Password string
}

// Gets [steamCredentials] from the environment.
// Raises an error if credentials cannot be obtained from the environment.
func getEnvSteamCredentials() (steamCredentials, error) {
	fail := func(err error) (steamCredentials, error) {
		return steamCredentials{}, err
	}

	logger.Info("get steam credentials from environment")
	password := os.Getenv(envSteamPassword)
	if password == "" {
		return fail(fmt.Errorf("env var %s unset", envSteamPassword))
	}

	username := os.Getenv(envSteamUsername)
	if username == "" {
		return fail(fmt.Errorf("env var %s unset", envSteamUsername))
	}

	return steamCredentials{Password: password, Username: username}, nil
}

// Gets application info for the given app id.  Returns a map containing the app info.
// Raises an error if the command to fetch the app information fails.
// Raises an error if the output from the command is unparseable.
func getSteamAppInfo(appId string, credentials steamCredentials) (map[string]any, error) {
	fail := func(err error) (map[string]any, error) {
		return map[string]any{}, err
	}

	logger.Info("get steam app info", "appId", steamAppId)

	// call steamcmd to obtain app info vdf
	output, err := runCmd([]string{
		"steamcmd",
		"+login", credentials.Username, credentials.Password,
		"+app_info_print", appId,
		"+quit",
	})
	if err != nil {
		return fail(err)
	}

	// extract app info vdf from steamcmd output
	appInfoString := ""
	marker := fmt.Sprintf("\"%s\"", appId)
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		if !strings.HasPrefix(line, marker) {
			continue
		}
		appInfoString = strings.Join(lines[index:], "\n")
		break
	}
	if appInfoString == "" {
		return fail(fmt.Errorf("data not found in steamcmd output"))
	}

	// parse vdf
	parser := vdf.NewParser(strings.NewReader(appInfoString))
	parsed, err := parser.Parse()
	if err != nil {
		return fail(err)
	}
	appInfo, ok := parsed[appId].(map[string]any)
	if !ok {
		return fail(fmt.Errorf("app id %s not found in app info", appId))
	}

	return appInfo, nil
}

// Gets the manifest id for a depot and branch within the provided app info map.
// Raises an error if any key within the nested maps required to fetch the manifest id are missing.
func getCurrentSteamManifestId(appInfo map[string]any, depotId string, branch string) (string, error) {
	fail := func(err error) (string, error) {
		return "", err
	}

	logger.Info("get current steam manifest id", "depotId", steamDepotId, "branch", steamBranchName)

	depots, ok := appInfo["depots"].(map[string]any)
	if !ok {
		return fail(fmt.Errorf("app info contains no depots"))
	}

	depot, ok := depots[depotId].(map[string]any)
	if !ok {
		return fail(fmt.Errorf("depot %s not found", depotId))
	}

	manifestsData, ok := depot["manifests"].(map[string]any)
	if !ok {
		return fail(fmt.Errorf("depot %s contains no manifests", depotId))
	}

	manifestData, ok := manifestsData[branch].(map[string]any)
	if !ok {
		return fail(fmt.Errorf("depot %s does not contain branch %s", depotId, branch))
	}

	manifestId, ok := manifestData["gid"].(string)
	if !ok {
		return fail(fmt.Errorf("depot %s, branch %s does not contain manifest gid", depotId, branch))
	}

	logger.Info("get current steam manifest id result", "manifestId", manifestId)

	return manifestId, nil
}

// githubCredentials represent information used to authenticate against github's http apis.
type githubCredentials struct {
	Token string
}

// Gets [githubCredentials] from the environment.
// Raises an error if credentials cannot be obtained from the environment.
func getEnvGithubCredentials() (githubCredentials, error) {
	fail := func(err error) (githubCredentials, error) {
		return githubCredentials{}, err
	}

	logger.Info("get github credentials from environment")

	token := os.Getenv(envGithubToken)
	if token == "" {
		return fail(fmt.Errorf("env var %s unset", envGithubToken))
	}

	return githubCredentials{Token: token}, nil
}

// githubWorkflowRun is information related to a fetched github workflow run.
type githubWorkflowRun struct {
	id string
}

// Fetches a github workflow run via github's http apis.  Assumes the workflow run's name is a manifest id (matching [manifestId]).  Returns a zero value if no github workflow runs could be found.
// Returns an error if the github http apis fail.
func getGithubWorkflowRun(owner string, repo string, workflowFilename string, manifestId string, credentials githubCredentials) (githubWorkflowRun, error) {
	fail := func(err error) (githubWorkflowRun, error) {
		return githubWorkflowRun{}, err
	}

	logger.Info("get github workflow run", "owner", owner, "repo", repo, "workflowFilename", workflowFilename, "manifestId", manifestId)

	client := github.NewClient(nil).WithAuthToken(credentials.Token)
	runs, _, err := client.Actions.ListWorkflowRunsByFileName(context.Background(), owner, repo, workflowFilename, &github.ListWorkflowRunsOptions{})
	if err != nil {
		return fail(err)
	}

	found := githubWorkflowRun{}
	for _, run := range runs.WorkflowRuns {
		if run.Name != nil && *run.Name == manifestId {
			found = githubWorkflowRun{id: strconv.Itoa(int(*run.ID))}
			break
		}
	}

	logger.Info("get github workflow run result", "id", found.id)

	return found, nil
}

// Creates a github workflow run using github's http apis.  Provides [manifestId] as an input (manifest_id) to to the workflow run.  Polls for the created workflow run until its found.
// Raises an error if the workflow run creation fails.
// Raises an error if attempts to fetch the workflow run fails.
func createGithubWorkflowRun(owner string, repo string, workflowFilename string, manifestId string, credentials githubCredentials) (githubWorkflowRun, error) {
	fail := func(err error) (githubWorkflowRun, error) {
		return githubWorkflowRun{}, err
	}

	logger.Info("create github workflow run", "owner", owner, "repo", repo, "workflowFilename", workflowFilename, "manifestId", manifestId)

	client := github.NewClient(nil).WithAuthToken(credentials.Token)
	_, err := client.Actions.CreateWorkflowDispatchEventByFileName(context.Background(), owner, repo, workflowFilename, github.CreateWorkflowDispatchEventRequest{
		Ref: "main",
		Inputs: map[string]interface{}{
			"manifest_id": manifestId,
		},
	})
	if err != nil {
		return fail(err)
	}

	var run githubWorkflowRun
	for {
		run, err = getGithubWorkflowRun(owner, repo, workflowFilename, manifestId, credentials)
		if err != nil {
			return fail(err)
		}
		if (run != githubWorkflowRun{}) {
			break
		}
		time.Sleep(10 * time.Second)
	}

	logger.Info("create github workflow run result", "runId", run.id)

	return run, nil
}

// Performs the entire auto-publish workflow.
// Raises an error if any function call fails.
func autoPublish() error {
	steamCredentials, err := getEnvSteamCredentials()
	if err != nil {
		return err
	}

	githubCredentials, err := getEnvGithubCredentials()
	if err != nil {
		return err
	}

	appInfo, err := getSteamAppInfo(steamAppId, steamCredentials)
	if err != nil {
		return err
	}

	manifestId, err := getCurrentSteamManifestId(appInfo, steamDepotId, steamBranchName)
	if err != nil {
		return err
	}

	manifestWorkflowRun, err := getGithubWorkflowRun(githubOwner, githubRepo, githubWorkflowFilename, manifestId, githubCredentials)
	if err != nil {
		return err
	}

	if manifestWorkflowRun == (githubWorkflowRun{}) {
		_, err := createGithubWorkflowRun(githubOwner, githubRepo, githubWorkflowFilename, manifestId, githubCredentials)
		if err != nil {
			return err
		}
	}

	return nil
}

// The main entrypoint for the script.  Calls [autoPublish] and handles any errors.
func main() {
	err := autoPublish()

	code := 0
	if err != nil {
		logger.Error("error while running autopublish", "msg", err.Error())
		code = 1
	}

	os.Exit(code)
}
