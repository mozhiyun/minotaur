package server

import (
	"github.com/kercylan98/minotaur/utils/log"
	"github.com/kercylan98/minotaur/utils/timer"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"reflect"
)

const (
	// WebsocketMessageTypeText 表示文本数据消息。文本消息负载被解释为 UTF-8 编码的文本数据
	WebsocketMessageTypeText = 1
	// WebsocketMessageTypeBinary 表示二进制数据消息
	WebsocketMessageTypeBinary = 2
	// WebsocketMessageTypeClose 表示关闭控制消息。可选消息负载包含数字代码和文本。使用 FormatCloseMessage 函数来格式化关闭消息负载
	WebsocketMessageTypeClose = 8
	// WebsocketMessageTypePing 表示 ping 控制消息。可选的消息负载是 UTF-8 编码的文本
	WebsocketMessageTypePing = 9
	// WebsocketMessageTypePong 表示一个 pong 控制消息。可选的消息负载是 UTF-8 编码的文本
	WebsocketMessageTypePong = 10
)

type Option func(srv *Server)

// WithTicker 通过定时器创建服务器，为服务器添加定时器功能
//   - autonomy：定时器是否独立运行（独立运行的情况下不会作为服务器消息运行，会导致并发问题）
//   - 多核与分流情况下需要考虑是否有必要 autonomy
func WithTicker(size int, autonomy bool) Option {
	return func(srv *Server) {
		if !autonomy {
			srv.ticker = timer.GetTicker(size)
		} else {
			srv.ticker = timer.GetTicker(size, timer.WithCaller(func(name string, caller func()) {
				srv.PushMessage(MessageTypeTicker, caller)
			}))
		}
	}
}

// WithCross 通过跨服的方式创建服务器
//   - 推送跨服消息时，将推送到对应crossName的跨服中间件中，crossName可以满足不同功能采用不同的跨服/消息中间件
//   - 通常情况下crossName仅需一个即可
func WithCross(crossName string, serverId int64, cross Cross) Option {
	return func(srv *Server) {
		srv.id = serverId
		if srv.cross == nil {
			srv.cross = map[string]Cross{}
		}
		srv.cross[crossName] = cross
		err := cross.Init(srv, func(serverId int64, packet []byte) {
			srv.PushMessage(MessageTypeCross, serverId, packet)
		})
		if err != nil {
			log.Error("WithCross", zap.Int64("ServerID", serverId), zap.String("Cross", reflect.TypeOf(cross).String()))
			panic(err)
		}
	}
}

// WithTLS 通过安全传输层协议TLS创建服务器
//   - 支持：Http、Websocket
func WithTLS(certFile, keyFile string) Option {
	return func(srv *Server) {
		switch srv.network {
		case NetworkHttp, NetworkWebsocket:
			srv.certFile = certFile
			srv.keyFile = keyFile
		}
	}
}

// WithGRPCServerOptions 通过GRPC的可选项创建GRPC服务器
func WithGRPCServerOptions(options ...grpc.ServerOption) Option {
	return func(srv *Server) {
		if srv.network != NetworkGRPC {
			return
		}
		srv.grpcServer = grpc.NewServer(options...)
	}
}

// WithProd 通过生产模式运行服务器
func WithProd() Option {
	return func(srv *Server) {
		srv.prod = true
	}
}

// WithWebsocketWriteMessageType 设置客户端写入的Websocket消息类型
//   - 默认： WebsocketMessageTypeBinary
func WithWebsocketWriteMessageType(messageType int) Option {
	return func(srv *Server) {
		switch messageType {
		case WebsocketMessageTypeText, WebsocketMessageTypeBinary, WebsocketMessageTypeClose, WebsocketMessageTypePing, WebsocketMessageTypePong:
			srv.websocketWriteMessageType = messageType
		default:
			log.Warn("WithWebsocketWriteMessageType", zap.Int("MessageType", messageType), zap.Error(ErrWebsocketMessageTypeException))
		}
	}
}

// WithWebsocketMessageType 设置仅支持特定类型的Websocket消息
func WithWebsocketMessageType(messageTypes ...int) Option {
	return func(srv *Server) {
		if srv.network != NetworkWebsocket {
			log.Warn("WitchWebsocketMessageType", zap.String("Network", string(srv.network)), zap.Error(ErrNotWebsocketUseMessageType))
			return
		}
		var supports = make(map[int]bool)
		for _, messageType := range messageTypes {
			switch messageType {
			case WebsocketMessageTypeText, WebsocketMessageTypeBinary, WebsocketMessageTypeClose, WebsocketMessageTypePing, WebsocketMessageTypePong:
				supports[messageType] = true
			default:
				log.Warn("WitchWebsocketMessageType", zap.Int("MessageType", messageType), zap.Error(ErrWebsocketMessageTypeException))
			}
		}
		srv.supportMessageTypes = supports
	}
}

// WithMessageBufferSize 通过特定的消息缓冲池大小运行服务器
//   - 默认大小为 4096 * 1024
//   - 消息数量超出这个值的时候，消息处理将会造成更大的开销（频繁创建新的结构体），同时服务器将输出警告内容
func WithMessageBufferSize(size int) Option {
	return func(srv *Server) {
		if size <= 0 {
			return
		}
		srv.messagePoolSize = size
	}
}

// WithMultiCore 通过特定核心数量运行服务器，默认为单核
//   - count > 1 的情况下，将会有对应数量的 goroutine 来处理消息
//   - 注意：HTTP和GRPC网络模式下不会生效
//   - 在需要分流的场景推荐采用多核模式，如游戏以房间的形式进行，每个房间互不干扰，这种情况下便可以每个房间单独维护数据包消息进行处理
func WithMultiCore(count int) Option {
	return func(srv *Server) {
		srv.core = count
		if srv.core < 1 {
			log.Warn("WithMultiCore", zap.Int("count", count), zap.String("tips", "wrong core count configuration, corrected to 1, currently in single-core mode"))
			srv.core = 1
		}
	}
}
