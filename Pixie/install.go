package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	catppuccin "github.com/catppuccin/go"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-git/go-git/v5"
	"golang.org/x/sys/windows"
	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	SourcePath string `yaml:"sourcePath"`
	DestPath   string `yaml:"destPath"`
}

type Config struct {
	RepoURL string               `yaml:"repoUrl"`
	Dirs    []string             `yaml:"dirs"`
	Pkgs    []string             `yaml:"pkgs"`
	Apps    map[string]AppConfig `yaml:"apps"`
}

type Logger struct {
	*log.Logger
	file *os.File
}

type Styles struct {
	Title     lipgloss.Style
	Success   lipgloss.Style
	Error     lipgloss.Style
	Info      lipgloss.Style
	Highlight lipgloss.Style
	Box       lipgloss.Style
}

var (
	cfg    Config
	logger *Logger
	styles Styles
)

func init() {
	var err error
	cfg, err = loadcfg("config.yml")
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	flavour := catppuccin.Mocha
	styles = Styles{
		Title:     lipgloss.NewStyle().Foreground(lipgloss.Color(flavour.Rosewater().Hex)).Bold(true).Padding(0, 1),
		Success:   lipgloss.NewStyle().Foreground(lipgloss.Color(flavour.Green().Hex)).Bold(true),
		Error:     lipgloss.NewStyle().Foreground(lipgloss.Color(flavour.Red().Hex)).Bold(true),
		Info:      lipgloss.NewStyle().Foreground(lipgloss.Color(flavour.Rosewater().Hex)),
		Highlight: lipgloss.NewStyle().Foreground(lipgloss.Color(flavour.Peach().Hex)).Underline(true),
		Box:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(flavour.Overlay0().Hex)),
	}
}

func loadcfg(filename string) (Config, error) {
	var config Config

	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("could not read config file: %w", err)
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return Config{}, fmt.Errorf("could not parse config file: %w", err)
	}

	return config, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println(styles.Error.Render(fmt.Sprintf("❌ Error: %v", err)))
		os.Exit(1)
	}
}

func run() error {
	if err := set(); err != nil {
		return fmt.Errorf("failed to set up logging: %w", err)
	}
	defer logger.file.Close()

	logger.Println("Pixie setup script started")

	fmt.Println(styles.Box.Render(styles.Title.Render("🦋 Starting the Installer")))

	if runtime.GOOS != "windows" {
		return fmt.Errorf("this script is designed to run on Windows only")
	}

	if !perm() {
		return fmt.Errorf("this script requires administrator privileges")
	}

	logger.log("🧚 Initialising Pixie", false)

	if err := inst("Chocolatey", ""); err != nil {
		return fmt.Errorf("failed to install Chocolatey: %w", err)
	}

	path, err := path()
	if err != nil {
		return fmt.Errorf("failed to get Documents path: %w", err)
	}

	if err := mk(path); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	tempDir, err := cp(path)
	if err != nil {
		return fmt.Errorf("failed to clone and copy repository: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := inst("Packages", tempDir); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}

	logger.log("🎉 All tasks completed successfully!", false)
	logger.Println("Pixie setup script completed successfully")

	if restart() {
		if err := clearstart(); err != nil {
			logger.log(fmt.Sprintf("Failed to clear startup folder: %v", err), true)
		}
		reboot()
	}

	return nil
}

func set() error {
	docPath, err := path()
	if err != nil {
		return fmt.Errorf("failed to get Documents path: %w", err)
	}

	logDir := filepath.Join(docPath, "Pixie")
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, "log.txt")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logger = &Logger{
		Logger: log.New(file, "", log.Ldate|log.Ltime|log.Lmicroseconds),
		file:   file,
	}
	return nil
}

func (l *Logger) log(message string, isError bool) {
	if isError {
		l.Printf("[ERROR] %s", message)
		fmt.Println(styles.Error.Render(fmt.Sprintf("❌ %s", message)))
	} else {
		l.Printf("[INFO] %s", message)
		fmt.Println(styles.Info.Render(message))
	}
}

func perm() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		logger.Printf("[ERROR] Failed to check admin status: %v", err)
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		logger.Printf("[ERROR] Failed to check admin status: %v", err)
		return false
	}
	return member
}

func inst(what string, tempDir string) error {
	switch what {
	case "Chocolatey":
		return choco()
	case "Packages":
		return pkgs(tempDir)
	default:
		return fmt.Errorf("unknown installation target: %s", what)
	}
}

func choco() error {
	logger.log("🍫 Installing Chocolatey...", false)

	cmd := exec.Command("powershell", "-Command", "Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))")

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("[ERROR] Failed to install Chocolatey: %v\nOutput: %s", err, string(output))
		return fmt.Errorf("failed to install Chocolatey: %w", err)
	}

	logger.Printf("[INFO] Chocolatey installation output: %s", string(output))

	if strings.Contains(string(output), "Chocolatey (choco.exe) is now ready.") {
		logger.log("✅ Chocolatey installed successfully", false)
	} else {
		logger.log("ℹ️ Chocolatey may have been installed, but the expected output was not found. Please check manually.", false)
	}

	return nil
}

func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Printf("[ERROR] Failed to get user home directory: %v", err)
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(home, "Documents"), nil
}

func mk(docPath string) error {
	for _, dir := range cfg.Dirs {
		path := filepath.Join(docPath, dir)
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			logger.Printf("[ERROR] Failed to create directory %s: %v", path, err)
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
		logger.log(fmt.Sprintf("📁 Created directory: %s", path), false)
	}
	return nil
}

func cp(docPath string) (string, error) {
	logger.log("📥 Cloning repository...", false)

	tempDir, err := os.MkdirTemp("", "configs")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	_, err = git.PlainClone(tempDir, false, &git.CloneOptions{
		URL:      cfg.RepoURL,
		Progress: io.Discard,
	})
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	logger.log("✅ Repository cloned successfully", false)

	srcWallpaperDir := filepath.Join(tempDir, "Wallpapers")
	destWallpaperDir := filepath.Join(docPath, "Wallpapers")
	if err := cpd(srcWallpaperDir, destWallpaperDir); err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to copy wallpapers: %w", err)
	}

	return tempDir, nil
}

func cpd(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		logger.Printf("[ERROR] Failed to read directory %s: %v", src, err)
		return fmt.Errorf("failed to read directory %s: %w", src, err)
	}

	if err := os.MkdirAll(dst, os.ModePerm); err != nil {
		logger.Printf("[ERROR] Failed to create destination directory %s: %v", dst, err)
		return fmt.Errorf("failed to create destination directory %s: %w", dst, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := cpd(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := cpf(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func cpf(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		logger.Printf("[ERROR] Failed to stat file %s: %v", src, err)
		return fmt.Errorf("failed to stat file %s: %w", src, err)
	}
	if !sourceFileStat.Mode().IsRegular() {
		logger.Printf("[ERROR] %s is not a regular file", src)
		return fmt.Errorf("%s is not a regular file", src)
	}
	source, err := os.Open(src)
	if err != nil {
		logger.Printf("[ERROR] Failed to open source file %s: %v", src, err)
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer source.Close()
	destination, err := os.Create(dst)
	if err != nil {
		logger.Printf("[ERROR] Failed to create destination file %s: %v", dst, err)
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer destination.Close()
	if _, err := io.Copy(destination, source); err != nil {
		logger.Printf("[ERROR] Failed to copy file from %s to %s: %v", src, dst, err)
		return fmt.Errorf("failed to copy file from %s to %s: %w", src, dst, err)
	}
	logger.log(fmt.Sprintf("🖼️ Copied file: %s", dst), false)
	return nil
}

func installvscext(extensionsFile string) error {
	file, err := os.Open(extensionsFile)
	if err != nil {
		logger.Printf("[ERROR] Failed to open extensions file %s: %v", extensionsFile, err)
		return fmt.Errorf("failed to open extensions file %s: %w", extensionsFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		extension := strings.TrimSpace(scanner.Text())
		if extension != "" {
			cmd := exec.Command("code", "--install-extension", extension)
			output, err := cmd.CombinedOutput()
			if err != nil {
				logger.Printf("[ERROR] Failed to install VSCode extension %s: %v\nOutput: %s", extension, err, string(output))
				return fmt.Errorf("failed to install VSCode extension %s: %w", extension, err)
			}
			logger.log(fmt.Sprintf("✅ Installed VSCode extension: %s", extension), false)
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Printf("[ERROR] Failed to read extensions file %s: %v", extensionsFile, err)
		return fmt.Errorf("failed to read extensions file %s: %w", extensionsFile, err)
	}

	return nil
}

func pkgs(tempDir string) error {
	if err := pkg("git"); err != nil {
		return fmt.Errorf("failed to install git: %w", err)
	}

	if err := cfggit(); err != nil {
		return fmt.Errorf("failed to configure git: %w", err)
	}

	if err := pkg("vscode"); err != nil {
		return fmt.Errorf("failed to install vscode: %w", err)
	}

	if err := cfgvsc(tempDir); err != nil {
		return fmt.Errorf("failed to configure vscode: %w", err)
	}

	if err := cfgapps(tempDir); err != nil {
		return fmt.Errorf("failed to configure applications: %w", err)
	}

	for _, pkgName := range cfg.Pkgs {
		if pkgName == "git" || pkgName == "vscode" {
			continue
		}
		if err := pkg(pkgName); err != nil {
			return fmt.Errorf("failed to install %s: %w", pkgName, err)
		}
	}

	return nil
}

func cfgvsc(tempDir string) error {
	logger.log("🔧 Configuring Visual Studio Code...", false)

	extensionsFile := filepath.Join(tempDir, "Pixie", "Apps", "Visual Studio Code", "extensions.txt")
	if err := installvscext(extensionsFile); err != nil {
		return fmt.Errorf("failed to install VSCode extensions: %w", err)
	}

	settingsSrc := filepath.Join(tempDir, "Pixie", "Apps", "Visual Studio Code", "settings.json")
	settingsDst := filepath.Join(os.Getenv("APPDATA"), "Code", "User", "settings.json")
	if err := cpf(settingsSrc, settingsDst); err != nil {
		return fmt.Errorf("failed to copy VSCode settings.json: %w", err)
	}

	logger.log("✅ Visual Studio Code configuration applied", false)
	return nil
}

func cfggit() error {
	logger.log("🔧 Configuring global git settings...", false)

	cmd := exec.Command("git", "config", "--global", "user.name", "aubrey")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("[ERROR] Failed to configure git user.name: %v\nOutput: %s", err, string(output))
		return fmt.Errorf("failed to configure git user.name: %w", err)
	}

	cmd = exec.Command("git", "config", "--global", "user.email", "cat@aubrey.rs")
	output, err = cmd.CombinedOutput()
	if err != nil {
		logger.Printf("[ERROR] Failed to configure git user.email: %v\nOutput: %s", err, string(output))
		return fmt.Errorf("failed to configure git user.email: %w", err)
	}

	logger.log("✅ Git global configuration applied", false)
	return nil
}

func cfgapps(tempDir string) error {
	logger.log("🔧 Configuring applications...", false)

	for appName, appConfig := range cfg.Apps {
		sourcePath := filepath.Join(tempDir, appConfig.SourcePath)
		destPath := os.ExpandEnv(appConfig.DestPath)

		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
			logger.log(fmt.Sprintf("Failed to create directory for %s: %v", appName, err), true)
			continue
		}

		if err := cpf(sourcePath, destPath); err != nil {
			logger.log(fmt.Sprintf("Failed to configure %s: %v", appName, err), true)
		} else {
			logger.log(fmt.Sprintf("✅ Configured %s", appName), false)
		}
	}

	logger.log("✅ Application configuration completed", false)
	return nil
}

func pkg(name string) error {
	logger.log(fmt.Sprintf("📦 Installing %s...", name), false)

	cmd := exec.Command("choco", "install", name, "-y")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("[ERROR] Failed to install %s: %v\nOutput: %s", name, err, string(output))
		return fmt.Errorf("failed to install %s: %w", name, err)
	}

	logger.Printf("[INFO] %s installation output: %s", name, string(output))

	if strings.Contains(string(output), "has been installed") {
		logger.log(fmt.Sprintf("✅ %s installed successfully", name), false)
	} else {
		logger.log(fmt.Sprintf("ℹ️ %s installation completed, but the expected output was not found. Please check manually.", name), false)
	}

	return nil
}

func clearstart() error {
	logger.log("🧹 Clearing startup folder...", false)

	startupFolders := []string{
		filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup"),
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup"),
	}

	for _, folder := range startupFolders {
		entries, err := os.ReadDir(folder)
		if err != nil {
			return fmt.Errorf("failed to read startup folder %s: %w", folder, err)
		}

		for _, entry := range entries {
			path := filepath.Join(folder, entry.Name())
			if err := os.Remove(path); err != nil {
				logger.log(fmt.Sprintf("Failed to remove %s: %v", path, err), true)
			} else {
				logger.log(fmt.Sprintf("Removed %s from startup", path), false)
			}
		}
	}

	logger.log("✅ Startup folder cleared", false)
	return nil
}

func restart() bool {
	reader := bufio.NewReader(os.Stdin)
	prompt := styles.Info.Render("🔄 Do you want to restart now? (y/n): ")
	fmt.Print(prompt)
	response, err := reader.ReadString('\n')
	if err != nil {
		logger.log(fmt.Sprintf("Error reading input: %v", err), true)
		return false
	}
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func reboot() {
	logger.log("🔄 Restarting the system...", false)
	cmd := exec.Command("shutdown", "/r", "/t", "0")
	err := cmd.Run()
	if err != nil {
		logger.log(fmt.Sprintf("Failed to restart: %v", err), true)
	}
}
