package camera

import (
	"camcontrol/device"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jacobsa/go-serial/serial"
)

// Using protocol Pelco-D - unfortunately there are several issues and many commands do not work with Tenveo camera!

type camera struct {
	deviceNo   byte               // camera device number (used to address target camera, mutliple camera could be used according protocol)
	portNo     int                // COM port number to use (configured one or -1 = search for first "USB-SERIAL CH340")
	port       io.ReadWriteCloser // serial port access
	simulation bool
}

// creatre camera object for Tenveo VN10U camera (use given device or search for first recognized device,
// e.g. windows assigns new port using different USB connector)
func NewTenveoNV10U(portNo int, deviceNo byte) (*camera, error) {
	log.Printf("'camera-control' setting: %v", os.Getenv("camera-control"))
	c := camera{
		portNo:     portNo,
		deviceNo:   deviceNo,
		simulation: os.Getenv("camera-control") == "simulation",
	}
	return &c, c.connect()
}

func (c *camera) connect() (err error) {
	if c.simulation {
		return nil
	}
	c.Close()
	portNo := getSerialPort(c.portNo)
	// search for COM0..COM255
	i := 0
	maxPort := 255

	var options serial.OpenOptions
	if portNo != -1 {
		i = portNo
		maxPort = i
	}
	for i <= maxPort {
		options = serial.OpenOptions{
			PortName:        "COM" + strconv.Itoa(i),
			BaudRate:        38400,
			DataBits:        8,
			StopBits:        1,
			MinimumReadSize: 1,
		}

		// Open the port.
		c.port, err = serial.Open(options)
		if err == nil {
			break
		}
		i++
	}
	if err != nil {
		portName := "COM"
		if portNo == -1 {
			portName += "0.." + strconv.Itoa(maxPort)
		} else {
			portName += strconv.Itoa(portNo)
		}
		err = fmt.Errorf("serial.Open: failed! port %v (%v)", portName, err)
		c.port = nil
	} else {
		log.Printf("serial.Open use port %v", options.PortName)
	}
	return
}

// search for device name "USB-SERIAL CH34<x> (COM<portNo>)"
func getSerialPort(portNo int) int {
	device.Init()
	defer device.Exit()
	prefix := "(COM"
	postfix := ")"
	devName := "USB-SERIAL CH34"
	for _, device := range device.GetDeviceClassPortNameList() {
		if strings.Contains(device, devName) {
			start := strings.Index(device, prefix)
			if start < 0 {
				continue
			}
			start += len(prefix)
			end := strings.Index(device[start:], postfix)
			if end < 0 {
				continue
			}
			end += start
			port, err := strconv.Atoi(device[start:end])
			if err != nil {
				log.Printf("Failed to read COM port in devicename: %v\n", device)
				continue
			}
			if portNo == -1 || portNo == port {
				return port
			}
		}
	}
	log.Printf("Failed to find device with name %s\n", devName)
	return -1
}

func (c *camera) Close() {
	if c.port != nil {
		_ = c.port.Close()
		c.port = nil
	}
}

const pan = 0
const tilt = 0

func (c *camera) Up() error {
	log.Println("Cam up")
	return c.sendCommand([]byte{0x00, 0x08, 0x00, tilt})
}

func (c *camera) Down() error {
	log.Println("Cam down")
	return c.sendCommand([]byte{0x00, 0x10, 0x00, tilt})
}

func (c *camera) Left() error {
	log.Println("Cam left")
	return c.sendCommand([]byte{0x00, 0x04, pan, 0x00})
}

func (c *camera) Right() error {
	log.Println("Cam right")
	return c.sendCommand([]byte{0x00, 0x02, pan, 0x00})
}

func (c *camera) PtStop() error {
	log.Println("Cam stop pt")
	return c.sendCommand([]byte{0x00, 0x00, 0x00, 0x00})
}

func (c *camera) ZoomIn(speed byte) error {
	log.Println("Cam zoom in")
	return c.sendCommand([]byte{0x00, 0x20, speed, 0x00})
}

func (c *camera) ZoomOut(speed byte) error {
	log.Println("Cam zoom out")
	return c.sendCommand([]byte{0x00, 0x40, speed, 0x00})
}

func (c *camera) ZoomStop() error {
	log.Println("Cam zoom stop")
	return c.sendCommand([]byte{0x00, 0x00, 0x00, 0x00})
}

/*
func (c *camera) FocusIn(speed byte) error {
	log.Println("Cam focus in")
	return c.sendCommand([]byte{0x01, 0x00, 0x00, 0x00})
}

func (c *camera) FocusOut(speed byte) error{
	log.Println("Cam focus out")
	return c.sendCommand([]byte{0x00, 0x80, 0x00, 0x00})
}

func (c *camera) FocusStop() error {
	log.Println("Cam focus stop")
	return c.sendCommand([]byte{0x00, 0x00, 0x00, 0x00})
}

func (c *camera) FocusAuto() error {
	log.Println("Cam focus auto")
	return c.sendCommand([]byte{0x10, 0x00, 0x00, 0x00})
}

func (c *camera) FocusManual() error {
	log.Println("Cam focus manual")
	return c.sendCommand([]byte{0x10, 0x00, 0x00, 0x00})
}
*/

func (c *camera) PresetSelect(preset byte) error {
	log.Printf("Cam preset select %d\n", preset)
	return c.sendCommand([]byte{0x00, 0x07, 0x00, preset})
}

func (c *camera) PresetSave(preset byte) error {
	log.Printf("Cam preset save %d\n", preset)
	return c.sendCommand([]byte{0x00, 0x03, 0x00, preset})
}

func (c *camera) PresetReset(preset byte) error {
	log.Println("Cam preset reset")
	return c.sendCommand([]byte{0x00, 0x05, 0x00, preset})
}

func calcChecksum(msg []byte) (checksum byte) {
	for _, v := range msg[1:] {
		checksum += v
	}
	return
}

// send command according protocol
func (c *camera) sendCommand(cmd []byte) error {
	header := []byte{0xff, c.deviceNo}
	msg := append(header, cmd...)
	msg = append(msg, calcChecksum(msg))

	if c.port == nil {
		err := c.connect()
		if err != nil {
			return fmt.Errorf("failed writing to port: %v", err)
		}
	}
	if c.port != nil {
		n, err := c.port.Write(msg)
		if err != nil {
			log.Printf("Failed writing to port: %v, try reconnect...", err)
			err := c.connect()
			if err != nil {
				return fmt.Errorf("failed writing to port: %v", err)
			}
			n, err = c.port.Write(msg)
			if err != nil {
				return fmt.Errorf("failed writing to port: %v", err)
			}
		}
		log.Printf("Wrote %d bytes: %s\n", n, hex.EncodeToString(msg))
		log.Printf("Response : %s\n", hex.EncodeToString(c.readResponse()))
	} else {
		log.Printf("Simulate request: %s\n", hex.EncodeToString(msg))
	}
	return nil
}

// read response (only for debugging, mostly there is no response and if, it is not according spec)
func (c *camera) readResponse() []byte {
	response := []byte{}
	if c.port == nil {
		return response
	}
	received := true
	for {
		val := make([]byte, 1)
		n, _ := c.port.Read(val)
		if n > 0 {
			response = append(response, val...)
			received = true
		} else {
			if received {
				time.Sleep(10 * time.Millisecond)
				received = false
			} else {
				return response
			}
		}
	}
}
