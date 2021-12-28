package device

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

type gUID struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

type sP_DEVINFO_DATA struct {
	cbSize    uint32
	classGuid gUID
	devInst   uint32
	reserved  *uint64
}

const (
	dIGCF_PRESENT        = uint32(0x2)
	iNVALID_HANDLE_VALUE = uintptr(0xffffffff)
	sPDRP_FRIENDLYNAME   = uintptr(0x0000000C)
)

var (
	setupDiGetClassDevs,
	setupDiGetDeviceRegistryProperty,
	setupDiEnumDeviceInfo uintptr
	gUID_DEVCLASS_PORTS = gUID{0x4d36e978, 0xe325, 0x11ce, [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}}
	setupApi            syscall.Handle
)

func Init() {
	setupApi, err := syscall.LoadLibrary("setupapi.dll")
	if err != nil {
		panic("LoadLibrary " + err.Error())
	}

	setupDiGetClassDevs = getProcAddr(setupApi, "SetupDiGetClassDevsA")
	setupDiEnumDeviceInfo = getProcAddr(setupApi, "SetupDiEnumDeviceInfo")
	setupDiGetDeviceRegistryProperty = getProcAddr(setupApi, "SetupDiGetDeviceRegistryPropertyA")
}

func Exit() {
	syscall.FreeLibrary(setupApi)
}

func getProcAddr(lib syscall.Handle, name string) uintptr {
	addr, err := syscall.GetProcAddress(lib, name)
	if err != nil {
		panic(name + " " + err.Error())
	}
	return addr
}

// read available device port "friendly" name of windows (LPTx/COMx)
func getDeviceClassPortName(device uint32) (result string, err error) {
	hDeviceInfo, _, err := syscall.Syscall6(setupDiGetClassDevs, 4, uintptr(unsafe.Pointer(&gUID_DEVCLASS_PORTS)), 0, 0, uintptr(dIGCF_PRESENT), 0, 0)
	if hDeviceInfo == iNVALID_HANDLE_VALUE || hDeviceInfo == 0 {
		return result, fmt.Errorf("setupDiGetClassDevs failed %v, %v", hDeviceInfo, err)
	}

	var dd sP_DEVINFO_DATA
	dd.cbSize = uint32(unsafe.Sizeof(dd))
	r1, _, err := syscall.Syscall(setupDiEnumDeviceInfo, 3, uintptr(hDeviceInfo), uintptr(device), uintptr(unsafe.Pointer(&dd)))
	if r1 == 0 {
		return result, fmt.Errorf("setupDiEnumDeviceInfo failed %v, %v", r1, err)
	}

	requiredSize := uint32(0)
	r1, _, err = syscall.Syscall9(setupDiGetDeviceRegistryProperty, 7, uintptr(hDeviceInfo), uintptr(unsafe.Pointer(&dd)), sPDRP_FRIENDLYNAME, 0, 0, 0, uintptr(unsafe.Pointer(&requiredSize)), 0, 0)
	if requiredSize == 0 {
		return result, fmt.Errorf("setupDiGetDeviceRegistryProperty requiredSize failed %v", err)
	}

	size := requiredSize
	p := make([]byte, size)
	r1, _, err = syscall.Syscall9(setupDiGetDeviceRegistryProperty, 7, uintptr(hDeviceInfo), uintptr(unsafe.Pointer(&dd)), sPDRP_FRIENDLYNAME, 0, uintptr(unsafe.Pointer(&p[0])), uintptr(size), 0, 0, 0)
	if r1 == 0 {
		return result, fmt.Errorf("setupDiGetDeviceRegistryProperty failed %v", err)
	}
	result = strings.Trim(*(*string)(unsafe.Pointer(&p)), " \n\t\x00")

	return result, nil
}

// helper for reading all available device port "friendly" names of windows (LPTx/COMx)
func GetDeviceClassPortNameList() []string {
	list := []string{}
	for dev := uint32(0); ; dev++ {
		if s, err := getDeviceClassPortName(dev); err == nil {
			list = append(list, s)
		} else {
			break
		}
	}
	return list
}
