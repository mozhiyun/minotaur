package component

// Lockstep 帧同步组件接口定义
type Lockstep[ClientID comparable, Command any] interface {
	// JoinClient 加入客户端
	JoinClient(client LockstepClient[ClientID])
	// JoinClientWithFrame 加入客户端，并且指定从特定帧开始
	JoinClientWithFrame(client LockstepClient[ClientID], frameIndex int)
	// LeaveClient 离开客户端
	LeaveClient(clientId ClientID)
	// StartBroadcast 开始广播
	StartBroadcast()
	// Stop 停止广播
	Stop()
	// AddCommand 增加指令
	AddCommand(command Command)
	// GetCurrentFrame 获取当前帧
	GetCurrentFrame() int
	// GetClientCurrentFrame 获取客户端当前帧
	GetClientCurrentFrame(clientId ClientID) int
	// GetFrameLimit 获取帧上限
	GetFrameLimit() int
	// GetFrames 获取所有帧数据
	GetFrames() [][]Command

	// RegLockstepStoppedEvent 当停止广播时将立即执行被注册的事件处理函数
	RegLockstepStoppedEvent(handle LockstepStoppedEventHandle[ClientID, Command])
	OnLockstepStoppedEvent()
}

type (
	// LockstepStoppedEventHandle 帧同步停止广播事件处理函数
	LockstepStoppedEventHandle[ClientID comparable, Command any] func(lockstep Lockstep[ClientID, Command])
)
