package main

import (
	"bufio"
	//"fmt"
	"encoding/json"
	"github.com/facchinm/go-serial"
	"github.com/kardianos/osext"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Download the file from URL first, store in tmp folder, then pass to spProgram
func spProgramFromUrl(portname string, boardname string, url string) {
	mapB, _ := json.Marshal(map[string]string{"ProgrammerStatus": "DownloadStart", "Url": url})
	h.broadcastSys <- mapB
	filename, err := downloadFromUrl(url)
	mapB, _ = json.Marshal(map[string]string{"ProgrammerStatus": "DownloadDone", "Filename": filename, "Url": url})
	h.broadcastSys <- mapB

	if err != nil {
		spErr(err.Error())
	} else {
		spProgram(portname, boardname, filename)
	}

	// delete file

}

func spProgram(portname string, boardname string, filePath string) {

	isFound, flasher, mycmd := assembleCompilerCommand(boardname, portname, filePath)
	mapD := map[string]string{"ProgrammerStatus": "CommandReady", "IsFound": strconv.FormatBool(isFound), "Flasher": flasher, "Cmd": strings.Join(mycmd, " ")}
	mapB, _ := json.Marshal(mapD)
	h.broadcastSys <- mapB

	if isFound {
		spHandlerProgram(flasher, mycmd)
	} else {
		spErr("We could not find the serial port " + portname + " or the board " + boardname + "  that you were trying to program.")
	}
}

func spHandlerProgram(flasher string, cmdString []string) {

	var oscmd *exec.Cmd
	// if runtime.GOOS == "darwin" {
	// 	sh, _ := exec.LookPath("sh")
	// 	// prepend the flasher to run it via sh
	// 	cmdString = append([]string{flasher}, cmdString...)
	// 	oscmd = exec.Command(sh, cmdString...)
	// } else {
	oscmd = exec.Command(flasher, cmdString...)
	// }

	// Stdout buffer
	//var cmdOutput []byte

	//h.broadcastSys <- []byte("Start flashing with command " + cmdString)
	log.Printf("Flashing with command:" + strings.Join(cmdString, " "))
	mapD := map[string]string{"ProgrammerStatus": "Starting", "Cmd": strings.Join(cmdString, " ")}
	mapB, _ := json.Marshal(mapD)
	h.broadcastSys <- mapB

	cmdOutput, err := oscmd.CombinedOutput()

	if err != nil {
		log.Printf("Command finished with error: %v "+string(cmdOutput), err)
		h.broadcastSys <- []byte("Could not program the board")
		mapD := map[string]string{"ProgrammerStatus": "Error", "Msg": "Could not program the board", "Output": string(cmdOutput)}
		mapB, _ := json.Marshal(mapD)
		h.broadcastSys <- mapB
	} else {
		log.Printf("Finished without error. Good stuff. stdout: " + string(cmdOutput))
		h.broadcastSys <- []byte("Flash OK!")
		mapD := map[string]string{"ProgrammerStatus": "Done", "Flash": "Ok", "Output": string(cmdOutput)}
		mapB, _ := json.Marshal(mapD)
		h.broadcastSys <- mapB
		// analyze stdin

	}
}

func formatCmdline(cmdline string, boardOptions map[string]string) (string, bool) {

	list := strings.Split(cmdline, "{")
	if len(list) == 1 {
		return cmdline, false
	}
	cmdline = ""
	for _, item := range list {
		item_s := strings.Split(item, "}")
		item = boardOptions[item_s[0]]
		if len(item_s) == 2 {
			cmdline += item + item_s[1]
		} else {
			if item != "" {
				cmdline += item
			} else {
				cmdline += item_s[0]
			}
		}
	}
	log.Println(cmdline)
	return cmdline, true
}

func containsStr(s []string, e string) bool {
	for _, a := range s {
		if strings.ToLower(a) == strings.ToLower(e) {
			return true
		}
	}
	return false
}

func assembleCompilerCommand(boardname string, portname string, filePath string) (bool, string, []string) {

	// get executable (self)path and use it as base for all other paths
	execPath, _ := osext.Executable()

	boardFields := strings.Split(boardname, ":")
	if len(boardFields) != 3 {
		h.broadcastSys <- []byte("Board need to be specified in core:architecture:name format")
		return false, "", nil
	}
	tempPath := (filepath.Dir(execPath) + "/" + boardFields[0] + "/hardware/" + boardFields[1] + "/boards.txt")
	file, err := os.Open(tempPath)
	if err != nil {
		h.broadcastSys <- []byte("Could not find board: " + boardname)
		log.Println("Error:", err)
		return false, "", nil
	}
	scanner := bufio.NewScanner(file)

	boardOptions := make(map[string]string)
	uploadOptions := make(map[string]string)

	for scanner.Scan() {
		// map everything matching with boardname
		if strings.Contains(scanner.Text(), boardFields[2]) {
			arr := strings.Split(scanner.Text(), "=")
			arr[0] = strings.Replace(arr[0], boardFields[2]+".", "", 1)
			boardOptions[arr[0]] = arr[1]
		}
	}

	if len(boardOptions) == 0 {
		h.broadcastSys <- []byte("Board " + boardFields[2] + " is not part of " + boardFields[0] + ":" + boardFields[1])
		return false, "", nil
	}

	boardOptions["serial.port"] = portname
	boardOptions["serial.port.file"] = filepath.Base(portname)

	// filepath need special care; the project_name var is the filename minus its extension (hex or bin)
	// if we are going to modify standard IDE files we also could pass ALL filename
	filePath = strings.Trim(filePath, "\n")
	boardOptions["build.path"] = filepath.Dir(filePath)
	boardOptions["build.project_name"] = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filepath.Base(filePath)))

	file.Close()

	// get infos about the programmer
	tempPath = (filepath.Dir(execPath) + "/" + boardFields[0] + "/hardware/" + boardFields[1] + "/platform.txt")
	file, err = os.Open(tempPath)
	if err != nil {
		h.broadcastSys <- []byte("Could not find board: " + boardname)
		log.Println("Error:", err)
		return false, "", nil
	}
	scanner = bufio.NewScanner(file)

	tool := boardOptions["upload.tool"]

	for scanner.Scan() {
		// map everything matching with upload
		if strings.Contains(scanner.Text(), tool) {
			arr := strings.Split(scanner.Text(), "=")
			uploadOptions[arr[0]] = arr[1]
			arr[0] = strings.Replace(arr[0], "tools."+tool+".", "", 1)
			boardOptions[arr[0]] = arr[1]
			// we have a "=" in command line
			if len(arr) > 2 {
				boardOptions[arr[0]] = arr[1] + "=" + arr[2]
			}
		}
	}
	file.Close()

	// multiple verisons of the same programmer can be handled if "version" is specified
	version := uploadOptions["runtime.tools."+tool+".version"]
	path := (filepath.Dir(execPath) + "/" + boardFields[0] + "/tools/" + tool + "/" + version)
	if err != nil {
		h.broadcastSys <- []byte("Could not find board: " + boardname)
		log.Println("Error:", err)
		return false, "", nil
	}

	boardOptions["runtime.tools."+tool+".path"] = path

	cmdline := boardOptions["upload.pattern"]
	// remove cmd.path as it is handled differently
	cmdline = strings.Replace(cmdline, "\"{cmd.path}\"", " ", 1)
	cmdline = strings.Replace(cmdline, "\"{path}/{cmd}\"", " ", 1)
	cmdline = strings.Replace(cmdline, "\"", "", -1)

	// split the commandline in substrings and recursively replace mapped strings
	cmdlineSlice := strings.Split(cmdline, " ")
	var winded = true
	for index, _ := range cmdlineSlice {
		winded = true
		for winded != false {
			cmdlineSlice[index], winded = formatCmdline(cmdlineSlice[index], boardOptions)
		}
	}

	// some boards (eg. Leonardo, Yun) need a special procedure to enter bootloader
	if boardOptions["upload.use_1200bps_touch"] == "true" {
		// triggers bootloader mode
		// the portname could change in this occasion, so fail gently
		log.Println("Restarting in bootloader mode")

		mode := &serial.Mode{
			BaudRate: 1200,
			Vmin:     1,
			Vtimeout: 0,
		}
		port, err := serial.OpenPort(portname, mode)
		if err != nil {
			log.Println(err)
			return false, "", nil
		}
		log.Println("Was able to open port in 1200 baud mode")
		//port.SetDTR(false)
		port.Close()
		time.Sleep(time.Second / 2)
		// time.Sleep(time.Second / 4)
		// wait for port to reappear
		if boardOptions["upload.wait_for_upload_port"] == "true" {
			ports, _ := serial.GetPortsList()
			log.Printf("Starting endless loop for portname:%v\n", portname)
			for !(containsStr(ports, portname)) {

				ports, _ = serial.GetPortsList()
			}
		}
	}

	tool = (filepath.Dir(execPath) + "/" + boardFields[0] + "/tools/" + tool + "/bin/" + tool)
	// the file doesn't exist, we are on windows
	if _, err := os.Stat(tool); err != nil {
		tool = tool + ".exe"
		// convert all "/" to "\"
		tool = strings.Replace(tool, "/", "\\", -1)
	}

	// remove blanks from cmdlineSlice
	var cmdlineSliceOut []string
	for _, element := range cmdlineSlice {
		if element != "" {
			cmdlineSliceOut = append(cmdlineSliceOut, element)
		}
	}

	log.Printf("Tool:%v, cmdline:%v\n", tool, cmdlineSliceOut)
	return (tool != ""), tool, cmdlineSliceOut
}
