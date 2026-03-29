package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"fileshare/internal/client"
	"fileshare/internal/discovery"
	"fileshare/internal/server"
)

// FileInfo is a type alias for server.FileInfo
type FileInfo = server.FileInfo

// App represents the main TUI application
type App struct {
	app         *tview.Application
	pages       *tview.Pages
	deviceList  *tview.List
	fileBrowser *tview.Table
	statusBar   *tview.TextView
	logView     *tview.TextView

	registry      *discovery.DeviceRegistry
	currentDevice *discovery.Device
	currentPath   string
	files         []FileInfo

	logChan  chan string
	modal    *tview.Modal
	quitting bool
	mu       sync.RWMutex
}

// NewApp creates a new TUI application
func NewApp(registry *discovery.DeviceRegistry) *App {
	app := &App{
		app:      tview.NewApplication(),
		pages:    tview.NewPages(),
		registry: registry,
		logChan:  make(chan string, 100),
	}

	app.setupUI()
	return app
}

// setupUI initializes the UI components
func (app *App) setupUI() {
	// Create the device list
	app.deviceList = tview.NewList().
		ShowSecondaryText(true).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkCyan)
	app.deviceList.SetBorder(true).
		SetTitle(" Devices ").
		SetTitleAlign(tview.AlignLeft)

	// Create the file browser table
	app.fileBrowser = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	app.fileBrowser.SetBorder(true).
		SetTitle(" Files (not connected) ").
		SetTitleAlign(tview.AlignLeft)

	// Set up header row
	app.fileBrowser.SetCell(0, 0, tview.NewTableCell("Name").
		SetTextColor(tcell.ColorYellow).
		SetSelectable(false))
	app.fileBrowser.SetCell(0, 1, tview.NewTableCell("Size").
		SetTextColor(tcell.ColorYellow).
		SetSelectable(false))
	app.fileBrowser.SetCell(0, 2, tview.NewTableCell("Modified").
		SetTextColor(tcell.ColorYellow).
		SetSelectable(false))

	// Create status bar
	app.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]q[white]: Quit | [yellow]Enter[white]: Select | [yellow]Backspace[white]: Up | [yellow]d[white]: Download | [yellow]r[white]: Refresh")
	app.statusBar.SetBorder(true).SetTitle(" Help ")

	// Create log view
	app.logView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	app.logView.SetBorder(true).SetTitle(" Log ")

	// Layout: device list on left, file browser on right, log at bottom
	leftPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(app.deviceList, 0, 1, true)

	rightPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(app.fileBrowser, 0, 1, false)

	mainContent := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(leftPanel, 35, 1, true).
		AddItem(rightPanel, 0, 1, false)

	bottomPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(app.logView, 10, 1, false).
		AddItem(app.statusBar, 3, 0, false)

	mainLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainContent, 0, 1, true).
		AddItem(bottomPanel, 13, 0, false)

	app.pages.AddPage("main", mainLayout, true, true)

	// Set up keyboard shortcuts for device list
	app.deviceList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			app.onDeviceSelected()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'r':
				go app.refreshDevices()
				return nil
			}
		}
		return event
	})

	// Set up keyboard shortcuts for file browser
	app.fileBrowser.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			app.onFileSelected()
			return nil
		case tcell.KeyBackspace, tcell.KeyEscape:
			app.navigateUp()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'd':
				app.downloadSelected()
				return nil
			case 'r':
				app.loadFiles()
				return nil
			}
		}
		return event
	})

	// Global shortcuts
	app.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.quit()
			return nil
		}
		
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'q', 'Q':
				app.quit()
				return nil
			case '1':
				app.pages.SwitchToPage("main")
				app.app.SetFocus(app.deviceList)
				return nil
			}
		}
		return event
	})

	app.app.SetRoot(app.pages, true)
}

// Run starts the application
func (app *App) Run() error {
	// Start log consumer
	go app.consumeLogs()

	app.app.SetRoot(app.pages, true)
	app.app.SetFocus(app.deviceList)
	
	// Initial refresh in background
	go app.refreshDevices()
	
	return app.app.Run()
}

// Stop stops the application
func (app *App) Stop() {
	app.app.Stop()
}

func (app *App) consumeLogs() {
	for message := range app.logChan {
		msg := message
		app.app.QueueUpdateDraw(func() {
			timestamp := time.Now().Format("15:04:05")
			fmt.Fprintf(app.logView, "[%s] %s\n", timestamp, msg)
			app.logView.ScrollToEnd()
		})
	}
}

// refreshDeviceList updates the device list from the registry
func (app *App) refreshDeviceList() {
	devices := app.registry.GetAll()

	app.app.QueueUpdateDraw(func() {
		app.deviceList.Clear()
		if len(devices) == 0 {
			app.deviceList.AddItem("No devices found", "Press 'r' to refresh", 0, nil)
			return
		}

		for _, device := range devices {
			d := device
			app.deviceList.AddItem(
				fmt.Sprintf("%s", device.HostName),
				fmt.Sprintf("%s:%d - %s", device.Addr, device.Port, device.SharedDir),
				0,
				func() {
					app.currentDevice = d
				},
			)
		}
	})
}

func (app *App) refreshDevices() {
	app.log("Refreshing devices...")
	app.refreshDeviceList()
}

func (app *App) onDeviceSelected() {
	index := app.deviceList.GetCurrentItem()
	devices := app.registry.GetAll()
	if index >= 0 && index < len(devices) {
		app.currentDevice = devices[index]
		app.currentPath = ""
		app.loadFiles()
		app.app.SetFocus(app.fileBrowser)
		app.log(fmt.Sprintf("Connected to %s", app.currentDevice.HostName))
	}
}

func (app *App) loadFiles() {
	if app.currentDevice == nil {
		return
	}

	app.fileBrowser.Clear()
	// Re-create header
	app.fileBrowser.SetCell(0, 0, tview.NewTableCell("Name").
		SetTextColor(tcell.ColorYellow).
		SetSelectable(false))
	app.fileBrowser.SetCell(0, 1, tview.NewTableCell("Size").
		SetTextColor(tcell.ColorYellow).
		SetSelectable(false))
	app.fileBrowser.SetCell(0, 2, tview.NewTableCell("Modified").
		SetTextColor(tcell.ColorYellow).
		SetSelectable(false))

	// Display loading message
	app.fileBrowser.SetCell(1, 0, tview.NewTableCell("Loading...").
		SetTextColor(tcell.ColorWhite))

	cli := client.NewDeviceClient(app.currentDevice.Addr, app.currentDevice.Port)
	
	// Load files in background to avoid blocking the UI
	go func() {
		files, err := cli.ListFiles(app.currentPath)
		
		app.app.QueueUpdateDraw(func() {
			if err != nil {
				app.log(fmt.Sprintf("Error loading files: %v", err))
				app.fileBrowser.Clear()
				app.fileBrowser.SetCell(0, 0, tview.NewTableCell("Name").
					SetTextColor(tcell.ColorYellow).
					SetSelectable(false))
				app.fileBrowser.SetCell(1, 0, tview.NewTableCell(fmt.Sprintf("[red]Error: %v", err)))
				return
			}

			app.mu.Lock()
			app.files = make([]FileInfo, len(files))
			copy(app.files, files)
			app.mu.Unlock()

			app.fileBrowser.Clear()
			// Re-create header
			app.fileBrowser.SetCell(0, 0, tview.NewTableCell("Name").
				SetTextColor(tcell.ColorYellow).
				SetSelectable(false))
			app.fileBrowser.SetCell(0, 1, tview.NewTableCell("Size").
				SetTextColor(tcell.ColorYellow).
				SetSelectable(false))
			app.fileBrowser.SetCell(0, 2, tview.NewTableCell("Modified").
				SetTextColor(tcell.ColorYellow).
				SetSelectable(false))

			for i, file := range files {
				row := i + 1
				icon := "[white]"
				if file.IsDir {
					icon = "[cyan]"
				}

				app.fileBrowser.SetCell(row, 0,
					tview.NewTableCell(fmt.Sprintf("%s%s", icon, file.Name)).
						SetTextColor(tcell.ColorDefault))
				app.fileBrowser.SetCell(row, 1,
					tview.NewTableCell(formatSize(file.Size)))
				app.fileBrowser.SetCell(row, 2,
					tview.NewTableCell(formatTime(file.ModTime)))
			}

			title := " Files "
			if app.currentPath != "" {
				title = fmt.Sprintf(" Files - /%s ", app.currentPath)
			}
			app.fileBrowser.SetTitle(title)
		})
	}()
}

func (app *App) onFileSelected() {
	row, _ := app.fileBrowser.GetSelection()
	if row <= 0 {
		return
	}

	app.mu.RLock()
	idx := row - 1
	if idx >= len(app.files) {
		app.mu.RUnlock()
		return
	}
	file := app.files[idx]
	app.mu.RUnlock()

	if file.IsDir {
		app.currentPath = file.Path
		app.loadFiles()
	} else {
		app.downloadFile(file)
	}
}

func (app *App) navigateUp() {
	if app.currentPath == "" {
		app.app.SetFocus(app.deviceList)
		return
	}

	// Go up one directory
	dir := filepath.Dir(app.currentPath)
	if dir == "." || dir == "/" || dir == "\\" {
		app.currentPath = ""
	} else {
		app.currentPath = dir
	}
	app.loadFiles()
}

func (app *App) downloadSelected() {
	row, _ := app.fileBrowser.GetSelection()
	if row <= 0 {
		return
	}

	app.mu.RLock()
	idx := row - 1
	if idx >= len(app.files) {
		app.mu.RUnlock()
		return
	}
	file := app.files[idx]
	app.mu.RUnlock()

	if !file.IsDir {
		app.downloadFile(file)
	}
}

func (app *App) downloadFile(file FileInfo) {
	if app.currentDevice == nil {
		return
	}

	// Determine download path
	downloadDir := "./downloads"
	if d := os.Getenv("FILESHARE_DOWNLOAD_DIR"); d != "" {
		downloadDir = d
	}
	localPath := filepath.Join(downloadDir, file.Name)

	app.log(fmt.Sprintf("Downloading %s to %s...", file.Name, localPath))

	// Create modal for progress
	app.modal = tview.NewModal().
		SetText(fmt.Sprintf("Downloading %s...", file.Name)).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			app.pages.RemovePage("modal")
			app.app.SetFocus(app.fileBrowser)
		})

	app.pages.AddPage("modal", app.modal, true, true)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cli := client.NewDeviceClient(app.currentDevice.Addr, app.currentDevice.Port)
		err := cli.DownloadFile(ctx, file.Path, localPath, nil)

		app.app.QueueUpdateDraw(func() {
			app.pages.RemovePage("modal")
			if err != nil {
				app.log(fmt.Sprintf("Download failed: %v", err))
				// Show error modal
				errorModal := tview.NewModal().
					SetText(fmt.Sprintf("Download failed:\n%v", err)).
					AddButtons([]string{"OK"}).
					SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						app.pages.RemovePage("error")
						app.app.SetFocus(app.fileBrowser)
					})
				app.pages.AddPage("error", errorModal, true, true)
			} else {
				app.log(fmt.Sprintf("Download complete: %s", localPath))
			}
		})
	}()
}

func (app *App) log(message string) {
	if message == "" {
		return
	}
	app.mu.RLock()
	if app.quitting {
		app.mu.RUnlock()
		return
	}
	app.mu.RUnlock()

	// Non-blocking send to log channel
	select {
	case app.logChan <- message:
	default:
		// Drop log if channel is full to avoid deadlock
	}
}

func (app *App) quit() {
	app.mu.Lock()
	if app.quitting {
		app.mu.Unlock()
		return
	}
	app.quitting = true
	app.mu.Unlock()
	
	// Close log channel
	close(app.logChan)
	
	app.app.Stop()
}

// Helper functions
func formatSize(size int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.1f GB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.1f MB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.1f KB", float64(size)/KB)
	default:
		if size < 0 {
			return "-"
		}
		return fmt.Sprintf("%d B", size)
	}
}

func formatTime(timeStr string) string {
	// Already formatted as RFC3339, just show date part
	if len(timeStr) >= 10 {
		return timeStr[:10]
	}
	return timeStr
}

// RefreshDevices periodically refreshes the device list
func (app *App) RefreshDevices(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			go app.refreshDevices()
		}
	}
}

// LogWriter returns an io.Writer that redirects logs to the log view
func (app *App) LogWriter() io.Writer {
	return &tuiLogWriter{app: app}
}

type tuiLogWriter struct {
	app *App
}

func (w *tuiLogWriter) Write(p []byte) (int, error) {
	w.app.log(strings.TrimSpace(string(p)))
	return len(p), nil
}
