package informer

type Message struct {
	Type        string      `json:"type"`
	MessageData MessageData `json:"messageData"`
}

type MessageData interface {
}

type InstanceMessage struct {
	IpAddress   string `json:"ipAddress"`
	Port        int32  `json:"port"`
	MessageData `json:"-"`
}

const InstanceAdded_MessageType = "INSTANCE_ADDED"
const InstanceRemoved_MessageType = "INSTANCE_REMOVED"
const InstanceRunning_MessageType = "INSTANCE_RUNNING"
const InstanceStopped_MessageType = "INSTANCE_STOPPED"
