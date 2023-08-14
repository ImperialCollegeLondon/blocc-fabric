package forknotifier

import (
	"github.com/hyperledger/fabric/common/flogging"
	"sync"
)

var (
	forkNotifications chan bool
	once              sync.Once
)

var logger = flogging.MustGetLogger("forknotifier")

func GetForkNotificationsChannel() chan bool {
	once.Do(func() {
		forkNotifications = make(chan bool)
	})
	return forkNotifications
}

func NotifyFork() {
	logger.Warningf("Fork detected, notifying...")
	forkNotifications <- true
	logger.Warningf("Fork notification sent")
}
