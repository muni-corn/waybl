package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/muni-corn/go-sway"
)

var globalWallpaper string
var blurAmount = "0x2"
var blurBools = make(map[string]bool)
var mtx = &sync.Mutex{}

func main() {
	outputWalls := make(map[string]string)

	// TODO check if the program is already running and
	// update it

	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	wayblDir := homeDir + "/.waybl"
	println("waybl dir: " + wayblDir)

	os.Mkdir(wayblDir, os.ModeDir|0755)

	// run through all of the arguments
	for i := 0; i < len(os.Args); i++ {
		if i == 0 {
			continue
		}

		arg := os.Args[i]

		switch arg {
		case "-b", "--blur":
			blurAmount = os.Args[i+1]
			i++
			continue
		}

		// if we have arguments such as "output-name:./wallpaper/image.png",
		// then we assign those to a map. otherwise, we
		// assume a global wallpaper argument has been
		// passed in. output-specific arguments take
		// precedence. the global wallpaper will not take
		// effect at all if output-specific wallpapers are
		// passed in.
		if strings.Contains(arg, ":") {
			split := strings.Split(arg, ":")
            output := split[0]
            wallpaperPath := os.ExpandEnv(split[1])
			outputWalls[output] = wallpaperPath
            go setWallpaper(output, wallpaperPath)
		} else {
			globalWallpaper = arg
            go setWallpaper("*", globalWallpaper)
		}
	}

	// this function will block until all wallpapers are
	// blurred
	makeWallpapers(wayblDir, outputWalls)

	// init
	checkEntireTree(wayblDir, outputWalls)

	// listen for window updates
	println("Listening...")
	swayEvents := sway.Subscribe(sway.WindowEventType, sway.WorkspaceEventType)
	for swayEvents.Next() {
		ev := swayEvents.Event()
		switch ev.(type) {
		case *sway.WindowEvent:
			windowEv := ev.(*sway.WindowEvent)
			if windowEv.Change != "title" {
				checkEntireTree(wayblDir, outputWalls)
			}
		case *sway.WorkspaceEvent:
			checkEntireTree(wayblDir, outputWalls)
		}
	}
}

func makeWallpapers(dir string, outputWalls map[string]string) {
	// a channel to monitor task completion
	done := make(chan bool)
	tasksLeft := 0

	for output, path := range outputWalls {
		tasksLeft += 2

		// start on blurred
		go func(output, path string) {
			println("starting to blur " + output)
			makeBlurred(dir, output, path)
			println("done blurring " + output)
			done <- true
		}(output, path)

		// start on cropped
		go func(output, path string) {
			println("starting to crop " + output)
			makeCropped(dir, output, path)
			println("done cropping " + output)
			done <- true
		}(output, path)
	}

	// blocks code until all wallpapers are blurred
	for tasksLeft > 0 && <-done {
		tasksLeft--
	}

	println("all wallpapers created!")
}

func makeCropped(dir, output, wallpaperPath string) {
	err := exec.Command(
		// TODO get geometry of output
		"convert", wallpaperPath,
		"-geometry", "1920x1080^",
		"-gravity", "center",
		"-crop", "1920x1080+0+0",
		getNormalWallpaperPath(output, dir),
	).Run()

	if err != nil {
		panic(err.Error())
	}
}

func makeBlurred(dir, output, wallpaperPath string) {
	err := exec.Command(
		// TODO get geometry of output
		"convert", wallpaperPath,
		"-geometry", "1920x1080^",
		"-gravity", "center",
		"-crop", "1920x1080+0+0",
		"-resize", "5%",
		"-blur", blurAmount,
		"-resize", "1000%",
		getBlurredWallpaperPath(output, dir),
	).Run()

	if err != nil {
		panic(err.Error())
	}
}

func setWallpaper(output, wallpaperPath string) {
	println("Setting wallpaper of " + output + " to " + wallpaperPath)

	retriesLeft := 5

	err := fmt.Errorf("no attempt at setting wallpaper yet")
	for err != nil && retriesLeft > 0 {
		cmd := exec.Command("swaymsg", "output", output, "bg", "\""+wallpaperPath+"\"", "fill", "#000000")
		cmdOutput, err := cmd.Output()
		println("cmd output: " + string(cmdOutput))

		if err != nil {
			println(err.Error())
			println("Retrying setWallpaper for " + output)
			time.Sleep(2 * time.Second)
			retriesLeft--

			// if error is still present after the set
			// amount of retries, give up
			if retriesLeft <= 0 {
				panic("wallpaper couldn't be set after 5 retries")
			}
			// continue
		} else {
			break
		}
	}
}

// also sets wallpaper
func setBlur(output string, newBlur bool, wayblDir string, outputWalls map[string]string) {
	if (blurBools[output] && newBlur) || (!blurBools[output] && !newBlur) {
		return
	}

	blurBools[output] = newBlur

	if newBlur {
		// set all wallpapers to blur
		println("blur on " + output + " is now on")
		setWallpaper(output, getBlurredWallpaperPath(output, wayblDir))
	} else {
		// set all wallpapers back to normal
		println("blur on " + output + " now off")
		setWallpaper(output, getNormalWallpaperPath(output, wayblDir))
	}
}

func getBlurredWallpaperPath(output string, wayblDir string) string {
	return wayblDir + "/" + output + "_blur.jpg"
}

func getNormalWallpaperPath(output string, wayblDir string) string {
	return wayblDir + "/" + output + ".jpg"
}

// returns true if a node in the tree is found to be focused
func isDescendantFocused(root *sway.Node) bool {
	switch root.Type {
	case sway.Con, sway.FloatingCon:
		// stop when we find a visible node
		if root.Visible {
			println("Visible node was:", root.Name)
			return true
		}
	}

	// recursive traversal if no visible nodes were found
	for _, n := range root.Nodes {
		if isDescendantFocused(n) {
			return true
		}
	}

	return false
}

func checkEntireTree(wayblDir string, outputWalls map[string]string) {
	tree, _ := sway.GetTree()
	checkOutputs(tree.Root.Nodes, wayblDir, outputWalls)
}

func checkOutputs(outputs []*sway.Node, wayblDir string, outputWalls map[string]string) {
	for _, o := range outputs {
		println("Checking " + o.Name)
		if o.Type == sway.OutputNode {
			go setBlur(o.Name, isDescendantFocused(o), wayblDir, outputWalls)
		}
	}
}
