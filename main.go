package main

//go:generate gotext -srclang=en update -out=./catalog.go -lang=en,de

import (
	"camcontrol/camera"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asticode/go-astikit"
	"github.com/asticode/go-astilectron"
)

var (
	VersionAstilectron string
	VersionElectron    string

	fs         = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	logfileArg = fs.String("LOGFILE", "log.txt", "the log filename")
	comPortArg = fs.Int("COMPORT", -1, "COM port of camera")
	profileArg = fs.String("PROFILE", "", "Overwrite last profile for pictures, e.g. 'church' or 'hut'")

	heightOffset = 0
	c            = &Context{size: astilectron.Size{Width: 1920, Height: 1080}}
)

const (
	windowHeight = 380
	windowWidth  = 350
	viewHtml     = "view.html"
	helpHtml     = "help.html"
	controlHtml  = "control.html"
)

// context required on events
type Context struct {
	dir        string
	uiDir      string
	uiView     string
	uiHelp     string
	uiControl  string
	symlink    string
	profiles   []string
	profile    string
	profileIdx int
	storeView  bool
	size       astilectron.Size
	cam        camera.Camera
	a          *astilectron.Astilectron
	mControl   *astilectron.MenuItem
	mHelp      *astilectron.MenuItem
	wView      *astilectron.Window
	wControl   *astilectron.Window
	wHelp      *astilectron.Window
}

func main() {
	// building with command line: astilectron-bundler.exe
	// copy ui and license folder to the binary directory

	// parse given arguments
	var err error
	fs.Parse(os.Args[1:])
	log.Printf("Camera_control called with: %v\n", os.Args)
	log.Printf("Use argument: LOGFILE=%v\n", *logfileArg)
	if len(*logfileArg) > 0 {
		var f *os.File
		f, err = os.OpenFile(*logfileArg, os.O_CREATE, 0666)
		if err != nil {
			err = fmt.Errorf("LOGFILE open failed: %v", err)
		} else {
			defer f.Close()
			log.SetOutput(f)
		}
	}

	// use last profile of symlink or use given argument
	current := "current"
	if err == nil {
		c.dir, err = getCurrentDir()
	}
	c.uiDir = filepath.Join(c.dir, "ui")
	c.uiView = filepath.Join(c.uiDir, viewHtml)
	c.uiHelp = filepath.Join(c.uiDir, helpHtml)
	c.uiControl = filepath.Join(c.uiDir, controlHtml)
	c.symlink = filepath.Join(c.uiDir, current)
	var e error // error without UI output
	if err == nil {
		c.profiles, err = getProfiles(c.uiDir, current)
	}
	if err == nil && len(c.profiles) != 0 {
		if c.profile, e = getCurrentProfile(c.uiDir, c.symlink); e != nil {
			log.Printf("getCurrentProfile failed: %v!\n", e)
		}
		if len(*profileArg) > 0 {
			log.Printf("Use argument: PROFILE=%v\n", *profileArg)
			if existsProfile(*profileArg, c.profiles) {

				if c.profile, e = setProfile(*profileArg, c.profile, c.uiDir, c.symlink); e != nil {
					log.Printf("Failed to set profile %v\n", e)
				}
			} else {
				log.Printf("Profile '%v' does not exist\n", *profileArg)
			}
		}
		if len(c.profile) != 0 && !existsProfile(c.profile, c.profiles) {
			log.Printf("Current profile '%v' does not exist\n", *profileArg)
			c.profile = ""
		}
		if len(c.profile) == 0 {
			log.Println("No valid profile, fallback to first entry")
			setProfile(c.profiles[0], c.profile, c.uiDir, c.symlink)
		}
		c.profileIdx = c.getProfileIndex()
	}

	var camerr error
	log.Printf("Use argument: COMPORT=%v\n", *comPortArg)
	c.cam, camerr = camera.NewTenveoNV10U(*comPortArg, 1)
	if camerr == nil {
		defer c.cam.Close()
	}

	// enable debugging in VS code
	os.Unsetenv("ELECTRON_RUN_AS_NODE")

	// create astilectron application
	var astierr error
	c.a, astierr = astilectron.New(log.Default(), astilectron.Options{
		AppName:            "Camera Control",
		AppIconDefaultPath: filepath.Join(c.uiDir, "icon.ico"),
		SingleInstance:     true,
	})
	if astierr != nil {
		log.Fatal(fmt.Errorf("main: Creating astilectron app failed: %w", astierr))
	}
	defer c.a.Close()

	// Handle signals
	c.a.HandleSignals()

	// Start
	if astierr = c.a.Start(); astierr != nil {
		log.Fatal(fmt.Errorf("main: Starting astilectron app failed: %w", astierr))
	}

	// use primary display (put window on right/bottom border)
	if c.a.PrimaryDisplay() != nil {
		c.size = c.a.PrimaryDisplay().Size()
	}
	log.Printf("Display size: %v\n", c.size)

	// create main window with menu
	c.createViewWindow()

	// it is required to adjust height on second call!? (new window on profile change)
	heightOffset = 40

	c.wView.SendMessage("test")
	if err != nil {
		c.wView.SendMessage("init-error-" + err.Error())
	}
	if camerr != nil {
		c.wView.SendMessage("io-error-" + camerr.Error())
	}

	// start event handling...
	c.a.Wait()
}

// create main window with menu
func (c *Context) createViewWindow() {
	var err error
	if c.wView, err = c.a.NewWindow(c.uiView, &astilectron.WindowOptions{
		Width:       astikit.IntPtr(windowWidth),
		Height:      astikit.IntPtr(windowHeight + heightOffset),
		X:           astikit.IntPtr(c.size.Width - windowWidth),
		Y:           astikit.IntPtr(c.size.Height - 2*windowHeight),
		Resizable:   astikit.BoolPtr(false),
		AlwaysOnTop: astikit.BoolPtr(true),
	}); err != nil {
		log.Fatal(fmt.Errorf("create new view window failed: %w", err))
	}
	c.wView.OnMessage(c.onWindowMessage)
	// handle control window synchron in case of events...
	c.wView.On(astilectron.EventNameWindowEventClosed, func(e astilectron.Event) (deleteListener bool) {
		destroy(c.wControl)
		destroy(c.wHelp)
		return true
	})
	c.wView.On(astilectron.EventNameWindowEventMinimize, func(e astilectron.Event) (deleteListener bool) {
		minimize(c.wControl)
		minimize(c.wHelp)
		return false
	})
	c.wView.On(astilectron.EventNameWindowEventRestore, func(e astilectron.Event) (deleteListener bool) {
		restore(c.wControl)
		restore(c.wHelp)
		return false
	})

	// create window
	if err = c.wView.Create(); err != nil {
		log.Fatal(fmt.Errorf("main: creatig view window failed: %w", err))
	}

	viewMissing := !fileExist(c.uiView)

	menuName := "Camera"
	enabled := true
	if viewMissing {
		menuName = "ui directory missing!"
		enabled = false
	}

	// setup menus (dynamically depending on profiles)
	menuOpt := []*astilectron.MenuItemOptions{
		{
			Label: astikit.StrPtr(menuName),
			SubMenu: []*astilectron.MenuItemOptions{
				{
					Label:   astikit.StrPtr("Control"),
					Checked: astikit.BoolPtr(false),
					Enabled: astikit.BoolPtr(enabled),
					Type:    astilectron.MenuItemTypeCheckbox,
					OnClick: c.onMenuControlClicked,
				},
			},
		},
	}
	if !viewMissing {
		if len(c.profiles) > 1 {
			subm := []*astilectron.MenuItemOptions{}
			for _, p := range c.profiles {
				subm = append(subm, &astilectron.MenuItemOptions{
					Checked: astikit.BoolPtr(strings.EqualFold(c.profile, p)),
					Label:   astikit.StrPtr(p),
					Type:    astilectron.MenuItemTypeRadio,
					OnClick: c.onMenuProfileClicked,
				})
			}
			menuOpt = append(menuOpt, &astilectron.MenuItemOptions{
				Label:   astikit.StrPtr("Profile"),
				SubMenu: subm,
			})
		}
		menuOpt = append(menuOpt, &astilectron.MenuItemOptions{
			Label: astikit.StrPtr("Help"),
			SubMenu: []*astilectron.MenuItemOptions{
				{
					Label:   astikit.StrPtr("Help"),
					Type:    astilectron.MenuItemTypeCheckbox,
					OnClick: c.onMenuHelpClicked,
				},
				{
					Label:   astikit.StrPtr("About"),
					Type:    astilectron.MenuItemTypeNormal,
					OnClick: c.onMenuAboutClicked,
				},
			},
		})
	}

	var m = c.a.NewMenu(menuOpt)
	c.mControl, _ = m.Item(0, 0)
	c.mHelp, _ = m.Item(2, 0)

	// create menu
	if err = m.Create(); err != nil {
		log.Fatal(fmt.Errorf("main: creatig menu failed: %w", err))
	}
}

func destroy(win *astilectron.Window) {
	if win != nil {
		win.Destroy()
	}
}

func minimize(win *astilectron.Window) {
	if win != nil {
		win.Minimize()
	}
}

func restore(win *astilectron.Window) {
	if win != nil {
		win.Restore()
	}
}

func existsProfile(profile string, profiles []string) bool {
	for _, p := range profiles {
		if strings.EqualFold(p, profile) {
			return true
		}
	}
	return false
}

func fileExist(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// estimate current profile of symlink
func getCurrentProfile(uiDir string, symlink string) (profile string, err error) {
	fi, err := os.Lstat(symlink)
	if err != nil {
		err = fmt.Errorf("os.Lstat of symlink %v failed: %v", symlink, err)
		return
	}

	if (fi.Mode() & os.ModeSymlink) == 0 {
		err = fmt.Errorf("no symlink found: %v", symlink)
		return
	}

	currentTarget, err := os.Readlink(symlink)
	if err != nil {
		err = fmt.Errorf("readlink of %v failed: %v", symlink, err)
		return
	}

	dir, profileCandidate := filepath.Split(currentTarget)
	if !strings.EqualFold(filepath.Clean(dir), uiDir) {
		err = fmt.Errorf("current profile dir is invalid: %v %v %v", filepath.Clean(dir), uiDir, profile)
		return
	}
	profile = profileCandidate
	log.Print("current profile:", profile)
	return
}

// read available profiles (all directories in folder "ui")
func getProfiles(uiDir string, ignore string) (profiles []string, err error) {
	files, err := ioutil.ReadDir(uiDir)
	if err != nil {
		err = fmt.Errorf("profile read dir failed: %v", err)
		return
	}
	for _, f := range files {
		if f.IsDir() && !strings.EqualFold(ignore, f.Name()) {
			profiles = append(profiles, f.Name())
		}
	}
	if len(profiles) == 0 {
		err = fmt.Errorf("no profiles found in '%v'", uiDir)
	}
	sort.Strings(profiles)
	return
}

// set new profile (update symlink to profile directory)
func setProfile(newProfile string, currentProfile string, uiDir string, symlink string) (string, error) {
	var err error
	if len(newProfile) <= 0 {
		log.Printf("No new profile specified, use last one: %s\n", currentProfile)
		return currentProfile, err
	}
	newProfile = filepath.Clean(newProfile)
	log.Printf("Set new profile '%v'\n", newProfile)
	if strings.EqualFold(newProfile, currentProfile) {
		log.Print("Symlink already available\n")
		return currentProfile, err
	}
	os.Remove(symlink)
	profileDir := filepath.Join(uiDir, newProfile)
	log.Printf("Create symlink to: %v\n", profileDir)
	err = os.Symlink(profileDir, symlink)
	if err != nil {
		log.Printf("Failed to create symlink: %v %v %v\n", profileDir, symlink, err)
		log.Printf("Probably FS does not support symlinks - fallback to copy")
		err = copyFiles(profileDir, symlink)
	}
	return newProfile, err
}

func copyFiles(srcDir string, destDir string) error {
	if !fileExist(destDir) {
		if err := os.Mkdir(destDir, 0755); err != nil {
			return err
		}
	}

	files, err := ioutil.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read dir failed %v: %v", srcDir, err)
	}
	for _, f := range files {
		if !f.IsDir() {
			var dest *os.File
			var src *os.File
			var err error
			d := filepath.Join(destDir, f.Name())
			if dest, err = os.Create(d); err != nil {
				err = fmt.Errorf("failed to create file %v: %v", d, err)
				return err
			}
			defer dest.Close()

			s := filepath.Join(srcDir, f.Name())
			if src, err = os.Open(s); err != nil {
				err = fmt.Errorf("failed to open file %v: %v", s, err)
				return err
			}
			defer src.Close()

			copyFile(src, dest)
		}
	}
	return nil
}

func copyFile(src *os.File, dest *os.File) error {
	buf := make([]byte, 65536)
	for {
		n, err := src.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}
		if _, err := dest.Write(buf[:n]); err != nil {
			return err
		}
	}
	return nil
}

// helper to retrieve current directory
func getCurrentDir() (dir string, err error) {
	dir, err = os.Getwd()
	if err != nil {
		err = fmt.Errorf("failed to get current dir: %v", err)
		return
	}
	log.Printf("Current dir: %v\n", dir)
	return
}

// calculate profile index (every profile uses different memory locations 0..8, 9..17, ...)
func (c *Context) getProfileIndex() int {
	for i, e := range c.profiles {
		if strings.EqualFold(e, c.profile) {
			return i
		}
	}
	return -1
}

// help menu handler
func (c *Context) onMenuHelpClicked(e astilectron.Event) bool {
	helpActive := *e.MenuItemOptions.Checked
	if helpActive {
		var err error
		if c.wHelp == nil {
			if c.wHelp, err = c.a.NewWindow(c.uiHelp, &astilectron.WindowOptions{
				Title:       astikit.StrPtr("Camera Control - HELP"),
				Width:       astikit.IntPtr(2 * windowWidth),
				Height:      astikit.IntPtr(2*windowHeight - 35),
				X:           astikit.IntPtr(c.size.Width - 3*windowWidth),
				Y:           astikit.IntPtr(c.size.Height - 2*windowHeight),
				Resizable:   astikit.BoolPtr(true),
				Minimizable: astikit.BoolPtr(true),
				AlwaysOnTop: astikit.BoolPtr(false),
			}); err != nil {
				log.Fatal(fmt.Errorf("new help window failed: %w", err))
			}
			c.wHelp.OnMessage(c.onWindowMessage)
			if err = c.wHelp.Create(); err != nil {
				log.Fatal(fmt.Errorf("create help window failed: %w", err))
			}
			c.wHelp.On(astilectron.EventNameWindowEventClosed, func(e astilectron.Event) (deleteListener bool) {
				if c.wHelp != nil && c.mHelp != nil {
					c.mHelp.SetChecked(false)
					c.wHelp = nil
				}
				return true
			})
			c.wHelp.On(astilectron.EventNameWindowEventRestore, func(e astilectron.Event) (deleteListener bool) {
				restore(c.wView)
				return false
			})
			c.wHelp.Show()
		}
	} else {
		destroy(c.wHelp)
	}
	return false
}

// about menu handler
func (c *Context) onMenuAboutClicked(e astilectron.Event) bool {
	c.wView.SendMessage("about")
	return false
}

// profile menu handler
func (c *Context) onMenuProfileClicked(e astilectron.Event) bool {
	c.wView.SendMessage("close-dialog")
	log.Printf("Profile change: '%s'\n", *e.MenuItemOptions.Label)
	var err error
	if c.profile, err = setProfile(*e.MenuItemOptions.Label, c.profile, c.uiDir, c.symlink); err != nil {
		log.Printf("Failed to set profile %v\n", err)
	}
	c.profileIdx = c.getProfileIndex()
	oldView := c.wView
	c.createViewWindow()
	oldView.Close()
	return true
}

// control menu handler (open/close control window)
func (c *Context) onMenuControlClicked(e astilectron.Event) bool {
	c.wView.SendMessage("close-dialog")
	controlActive := *e.MenuItemOptions.Checked
	log.Printf("Control active: %v\n", controlActive)
	if controlActive {
		var err error
		if c.wControl == nil {
			if c.wControl, err = c.a.NewWindow(c.uiControl, &astilectron.WindowOptions{
				Title:       astikit.StrPtr("Camera Control - CONTROL"),
				Width:       astikit.IntPtr(windowWidth),
				Height:      astikit.IntPtr(windowHeight),
				X:           astikit.IntPtr(c.size.Width - windowWidth),
				Y:           astikit.IntPtr(c.size.Height - windowHeight),
				Resizable:   astikit.BoolPtr(false),
				Minimizable: astikit.BoolPtr(false),
				AlwaysOnTop: astikit.BoolPtr(true),
			}); err != nil {
				log.Fatal(fmt.Errorf("new control window failed: %w", err))
			}
			c.wControl.OnMessage(c.onWindowMessage)
			if err = c.wControl.Create(); err != nil {
				log.Fatal(fmt.Errorf("create control window failed: %w", err))
			}
			c.wControl.On(astilectron.EventNameWindowEventClosed, func(e astilectron.Event) (deleteListener bool) {
				if c.wControl != nil && c.mControl != nil {
					c.mControl.SetChecked(false)
					c.wControl = nil
				}
				return true
			})
			c.wControl.On(astilectron.EventNameWindowEventRestore, func(e astilectron.Event) (deleteListener bool) {
				c.wView.Restore()
				return false
			})
		}
		c.storeViewOff(true)
	} else {
		if c.wControl != nil {
			destroy(c.wControl)
			c.storeViewOff(false)
		}
	}
	return false
}

// switch "store view" off - to avoid accidently overwriting views
func (c *Context) storeViewOff(show bool) {
	if c.wControl != nil {
		c.wControl.SendMessage("store:off")
		c.storeView = false
		if show && !c.wControl.IsShown() {
			c.wControl.Show()
		}
	}
}

// camer control event received: pan, tilt, zoom, store, recall camera...
func (c *Context) onWindowMessage(m *astilectron.EventMessage) interface{} {
	// Unmarshal
	var elementId string
	var err error
	var n int
	m.Unmarshal(&elementId)
	if strings.HasPrefix(elementId, "ctrl_") {
		// up/down/left/righ/zoom...
		idx := 6
		if strings.HasPrefix(elementId, "ctrl_x") {
			idx++
		}
		n, err = strconv.Atoi(elementId[idx:])
		ctrl := byte(n & 0xff)

		if err != nil || int(ctrl) != n {
			log.Printf("Failed to convert to int %v %v\n", elementId, err)
			return nil
		}
		c.storeViewOff(false)
		fine := strings.HasPrefix(elementId, "ctrl_b") || strings.HasPrefix(elementId, "ctrl_xb")
		log.Printf("Fine: %v\n", fine)
		speed := byte(0x1f)
		if fine {
			speed = byte(0x02)
		}
		switch ctrl {
		case 1:
			err = c.cam.Left()
		case 2:
			err = c.cam.Right()
		case 3:
			err = c.cam.Up()
		case 4:
			err = c.cam.Down()
		case 5:
			err = c.cam.ZoomIn(speed)
		case 6:
			err = c.cam.ZoomOut(speed)
		}
		if err == nil {
			if fine {
				time.Sleep(5 * time.Millisecond)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
			if ctrl <= 4 {
				c.cam.PtStop()
			} else {
				c.cam.ZoomStop()
			}
		}
	} else if strings.HasPrefix(elementId, "view") {
		// view select 1..9
		n, err = strconv.Atoi(elementId[4:])
		preset := byte(n & 0xff)

		if err != nil || int(preset) != n {
			log.Printf("Failed to convert to int %v %v\n", elementId, err)
			return nil
		}
		store := c.storeView
		c.storeViewOff(false)
		if c.profileIdx > 0 {
			preset = preset + 9*byte(c.profileIdx)
		}

		if store {
			log.Printf("Save Preset: %d\n", preset)
			err = c.cam.PresetSave(preset)
		} else {
			log.Printf("Activate Preset: %d\n", preset)
			err = c.cam.PresetSelect(preset)
		}

	} else if strings.HasPrefix(elementId, "store") {
		// store on/off
		c.storeView = strings.EqualFold(elementId, "store:true")
	} else {
		log.Printf("Unknown event: %v\n", elementId)
	}
	if err != nil {
		log.Printf("Camera io-error: %v", err)
		c.wView.SendMessage("io-error-" + err.Error())
	}
	return nil
}
