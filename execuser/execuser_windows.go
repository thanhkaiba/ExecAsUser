package execuser

import (
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modwtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modadvapi32 = windows.NewLazySystemDLL("advapi32.dll")
	moduserenv  = windows.NewLazySystemDLL("userenv.dll")

	procWTSEnumerateSessionsW        = modwtsapi32.NewProc("WTSEnumerateSessionsW")
	procWTSGetActiveConsoleSessionId = modkernel32.NewProc("WTSGetActiveConsoleSessionId")
	procWTSQueryUserToken            = modwtsapi32.NewProc("WTSQueryUserToken")
	procDuplicateTokenEx             = modadvapi32.NewProc("DuplicateTokenEx")
	procCreateEnvironmentBlock       = moduserenv.NewProc("CreateEnvironmentBlock")
	procCreateProcessAsUser          = modadvapi32.NewProc("CreateProcessAsUserW")
)

const (
	WtsCurrentServerHandle uintptr = 0
)

type SW int

const (
	SwShow = 5
)

type WtsSessionInfo struct {
	SessionID      windows.Handle
	WinStationName *uint16
	State          int
}

const (
	CreateUnicodeEnvironment uint16 = 0x00000400

	CreateNewConsole = 0x00000010
)

// Run uses the Windows API to run a child process as the current login user.
// It assumes the caller is running as a SYSTEM Windows service.
//
// It sets the environment of the current process so that it gets inherited by
// the child process (see call to CreateEnvironmentBlock).
// From https://docs.microsoft.com/en-us/windows/win32/procthread/changing-environment-variables:
//
//	"If you want the child process to inherit most of the parent's environment with
//	only a few changes, retrieve the current values using GetEnvironmentVariable, save these values,
//	create an updated block for the child process to inherit, create the child process, and then
//	restore the saved values using SetEnvironmentVariable, as shown in the following example."
func Run(path string) {
	startProcessAsCurrentUser(path, "", "")
}

// getCurrentUserSessionId will attempt to resolve
// the session ID of the user currently active on
// the system.
func getCurrentUserSessionId() ([]windows.Handle, error) {
	if sessionList, err := wtsEnumerateSessions(); err != nil {
		if err != nil {
			return nil, fmt.Errorf("get current user session token: %s", err)
		}

		var result []windows.Handle
		for i := range sessionList {
			if sessionList[i].State == windows.WTSActive {
				result = append(result, sessionList[i].SessionID)
			}
		}
		if len(result) > 0 {
			return result, nil
		}
	}

	if sessionId, _, err := procWTSGetActiveConsoleSessionId.Call(); sessionId == 0xFFFFFFFF {
		return nil, fmt.Errorf("get current user session token: call native WTSGetActiveConsoleSessionId: %s", err)
	} else {
		return []windows.Handle{windows.Handle(sessionId)}, nil
	}
}

// wtsEnumerateSession will call the native
// version for Windows and parse the result
// to a Golang friendly version
func wtsEnumerateSessions() ([]*WtsSessionInfo, error) {
	var (
		sessionInformation = windows.Handle(0)
		sessionCount       = 0
		sessionList        = make([]*WtsSessionInfo, 0)
	)

	if returnCode, _, err := procWTSEnumerateSessionsW.Call(WtsCurrentServerHandle, 0, 1, uintptr(unsafe.Pointer(&sessionInformation)), uintptr(unsafe.Pointer(&sessionCount))); returnCode == 0 {
		return nil, fmt.Errorf("call native WTSEnumerateSessionsW: %s", err)
	}

	structSize := unsafe.Sizeof(WtsSessionInfo{})
	current := uintptr(sessionInformation)
	for i := 0; i < sessionCount; i++ {
		sessionList = append(sessionList, (*WtsSessionInfo)(unsafe.Pointer(current)))
		current += structSize
	}

	return sessionList, nil
}

// duplicateUserTokenFromSessionID will attempt
// to duplicate the user token for the user logged
// into the provided session ID
func duplicateUserTokenFromSessionID(sessionId windows.Handle) (windows.Token, error) {
	var (
		impersonationToken windows.Handle = 0
		userToken          windows.Token  = 0
	)

	if returnCode, _, err := procWTSQueryUserToken.Call(uintptr(sessionId), uintptr(unsafe.Pointer(&impersonationToken))); returnCode == 0 {
		return 0xFFFFFFFF, fmt.Errorf("call native WTSQueryUserToken: %s", err)
	}

	if returnCode, _, err := procDuplicateTokenEx.Call(uintptr(impersonationToken), 0, 0, uintptr(windows.SecurityImpersonation), uintptr(windows.TokenPrimary), uintptr(unsafe.Pointer(&userToken))); returnCode == 0 {
		return 0xFFFFFFFF, fmt.Errorf("call native DuplicateTokenEx: %s", err)
	}

	if err := windows.CloseHandle(impersonationToken); err != nil {
		return 0xFFFFFFFF, fmt.Errorf("close windows handle used for token duplication: %s", err)
	}

	return userToken, nil
}

func startProcessAsCurrentUser(appPath, cmdLine, workDir string) {
	var (
		listSessionId []windows.Handle
		userToken     windows.Token
		envInfo       windows.Handle

		startupInfo windows.StartupInfo
		processInfo windows.ProcessInformation

		commandLine uintptr = 0
		workingDir  uintptr = 0

		err error
	)

	if listSessionId, err = getCurrentUserSessionId(); err != nil {
		log.Println(err)
	}
	fmt.Println(listSessionId)
	for _, sessionId := range listSessionId {
		if userToken, err = duplicateUserTokenFromSessionID(sessionId); err != nil {
			log.Printf("get duplicate user token for current user session: %s\n", err)
			continue
		}

		if returnCode, _, err := procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&envInfo)), uintptr(userToken), 1); returnCode == 0 {
			log.Printf("create environment details for process: %s\n", err)
			continue
		}

		creationFlags := CreateUnicodeEnvironment | CreateNewConsole
		startupInfo.ShowWindow = SwShow
		startupInfo.Desktop = windows.StringToUTF16Ptr("winsta0\\default")

		if len(cmdLine) > 0 {
			commandLine = uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(cmdLine)))
		}
		if len(workDir) > 0 {
			workingDir = uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(workDir)))
		}

		if returnCode, _, err := procCreateProcessAsUser.Call(
			uintptr(userToken), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(appPath))), commandLine, 0, 0, 0,
			uintptr(creationFlags), uintptr(envInfo), workingDir, uintptr(unsafe.Pointer(&startupInfo)), uintptr(unsafe.Pointer(&processInfo)),
		); returnCode == 0 {
			log.Printf("create process as user: %s\n", err)
		}
	}

}
