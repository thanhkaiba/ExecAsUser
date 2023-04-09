// Receive session change notifications from Windows

package session_notifications

// #cgo LDFLAGS: -lwtsapi32
/*
#include <windows.h>
extern HANDLE Start();
extern void Stop(HANDLE);
*/
import "C"

import (
	"golang.org/x/sys/windows"
	"log"
	"unsafe"
)

type Message struct {
	UMsg   int
	WParam int
	LParam int
	ChanOk chan int
}

var (
	chanMessages = make(chan Message, 1000)
)

//export relayMessage
func relayMessage(message C.uint, wParam C.uint, lParam C.uint) {
	msg := Message{
		UMsg:   int(message),
		WParam: int(wParam),
		LParam: int(lParam),
	}
	msg.ChanOk = make(chan int)

	chanMessages <- msg

	// wait for the app to do its thing
	// it's usefully for WM_QUERYENDSESSION if we need time to save before Windows shutdown
	<-msg.ChanOk
}

// Subscribe will make it so that subChan will receive the session events.
// chanSessionEnd will receive a '1' when the session ends (when Windows shut down)
// To unsubscribe, close closeChan
// You must close 'ChanOk' after processing the event. This channel is to give you time to save if the event is WM_QUERYENDSESSION
func Subscribe(subMessages chan Message, quit chan struct{}) {
	var threadHandle C.HANDLE
	threadHandle = C.Start()
	go func() {
		for {
			select {
			case <-quit:
				log.Println("da done")
				C.Stop(threadHandle)

				err := windows.CloseHandle(windows.Handle(unsafe.Pointer(threadHandle)))
				if err != nil {
					log.Println(err)
				}

				return
			case c := <-chanMessages:
				subMessages <- c
			}
		}
	}()
}
