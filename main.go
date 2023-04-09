package main

import (
	"exec_as_user/execuser"
	"exec_as_user/session_notifications"
	"golang.org/x/sys/windows"
)

func main() {

	execuser.Run("a_program.exe")
	quit := make(chan struct{})
	changes := make(chan session_notifications.Message, 100)
	go func() {
		for {
			select {
			case c := <-changes:
				switch c.WParam {
				case windows.WTS_SESSION_LOGON:
				case windows.WTS_SESSION_UNLOCK:
					execuser.Run("a_program.exe")
				}
				close(c.ChanOk)
			}
		}
	}()

	session_notifications.Subscribe(changes, quit)

	// ctrl+c to quit
	<-quit
}
