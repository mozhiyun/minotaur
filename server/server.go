package server

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/kercylan98/minotaur/utils/log"
	"github.com/kercylan98/minotaur/utils/synchronization"
	"github.com/kercylan98/minotaur/utils/timer"
	"github.com/panjf2000/gnet"
	"github.com/xtaci/kcp-go/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// New 根据特定网络类型创建一个服务器
func New(network Network, options ...Option) *Server {
	server := &Server{
		event:                     &event{},
		network:                   network,
		options:                   options,
		core:                      1,
		closeChannel:              make(chan struct{}),
		websocketWriteMessageType: WebsocketMessageTypeBinary,
	}
	server.event.Server = server

	if network == NetworkHttp {
		server.ginServer = gin.New()
		server.httpServer = &http.Server{
			Handler: server.ginServer,
		}
	} else if network == NetworkGRPC {
		server.grpcServer = grpc.NewServer()
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// Server 网络服务器
type Server struct {
	*event                               // 事件
	cross               map[string]Cross // 跨服
	id                  int64            // 服务器id
	network             Network          // 网络类型
	addr                string           // 侦听地址
	options             []Option         // 选项
	ginServer           *gin.Engine      // HTTP模式下的路由器
	httpServer          *http.Server     // HTTP模式下的服务器
	grpcServer          *grpc.Server     // GRPC模式下的服务器
	supportMessageTypes map[int]bool     // websocket模式下支持的消息类型
	certFile, keyFile   string           // TLS文件
	isShutdown          atomic.Bool      // 是否已关闭
	closeChannel        chan struct{}    // 关闭信号

	gServer                   *gNet                           // TCP或UDP模式下的服务器
	messagePool               *synchronization.Pool[*Message] // 消息池
	messagePoolSize           int                             // 消息池大小
	messageChannel            map[int]chan *Message           // 消息管道
	initMessageChannel        bool                            // 消息管道是否已经初始化
	multiple                  bool                            // 是否为多服务器模式下运行
	prod                      bool                            // 是否为生产模式
	core                      int                             // 消息处理核心数
	websocketWriteMessageType int                             // websocket写入的消息类型
	ticker                    *timer.Ticker                   // 定时器
}

// Run 使用特定地址运行服务器
//
//	server.NetworkTcp (addr:":8888")
//	server.NetworkTcp4 (addr:":8888")
//	server.NetworkTcp6 (addr:":8888")
//	server.NetworkUdp (addr:":8888")
//	server.NetworkUdp4 (addr:":8888")
//	server.NetworkUdp6 (addr:":8888")
//	server.NetworkUnix (addr:"socketPath")
//	server.NetworkHttp (addr:":8888")
//	server.NetworkWebsocket (addr:":8888/ws")
//	server.NetworkKcp (addr:":8888")
func (slf *Server) Run(addr string) error {
	if slf.event == nil {
		return ErrConstructed
	}
	slf.event.check()
	slf.addr = addr
	var protoAddr = fmt.Sprintf("%s://%s", slf.network, slf.addr)
	var connectionInitHandle = func(callback func()) {
		slf.initMessageChannel = true
		if slf.messagePoolSize <= 0 {
			slf.messagePoolSize = 100
		}
		slf.messagePool = synchronization.NewPool[*Message](slf.messagePoolSize,
			func() *Message {
				return &Message{}
			},
			func(data *Message) {
				data.t = 0
				data.attrs = nil
			},
		)
		slf.messageChannel = map[int]chan *Message{}
		for i := 0; i < slf.core; i++ {
			slf.messageChannel[i] = make(chan *Message, 4096*1000)
		}
		if slf.network != NetworkHttp && slf.network != NetworkWebsocket {
			slf.gServer = &gNet{Server: slf}
		}
		if callback != nil {
			go callback()
		}
		for _, messageChannel := range slf.messageChannel {
			messageChannel := messageChannel
			go func() {
				for message := range messageChannel {
					slf.dispatchMessage(message)
				}
			}()
		}
	}

	switch slf.network {
	case NetworkGRPC:
		listener, err := net.Listen(string(NetworkTcp), slf.addr)
		if err != nil {
			return err
		}
		go func() {
			slf.OnStartBeforeEvent()
			if err := slf.grpcServer.Serve(listener); err != nil {
				slf.PushMessage(MessageTypeError, err, MessageErrorActionShutdown)
			}
		}()
	case NetworkTcp, NetworkTcp4, NetworkTcp6, NetworkUdp, NetworkUdp4, NetworkUdp6, NetworkUnix:
		go connectionInitHandle(func() {
			slf.OnStartBeforeEvent()
			if err := gnet.Serve(slf.gServer, protoAddr); err != nil {
				slf.PushMessage(MessageTypeError, err, MessageErrorActionShutdown)
			}
		})
	case NetworkKcp:
		listener, err := kcp.ListenWithOptions(slf.addr, nil, 0, 0)
		if err != nil {
			return err
		}
		go connectionInitHandle(func() {
			slf.OnStartBeforeEvent()
			for {
				session, err := listener.AcceptKCP()
				if err != nil {
					continue
				}

				conn := newKcpConn(slf, session)
				slf.OnConnectionOpenedEvent(conn)

				go func(conn *Conn) {
					defer func() {
						if err := recover(); err != nil {
							slf.OnConnectionClosedEvent(conn)
						}
					}()

					buf := make([]byte, 4096)
					for {
						n, err := conn.kcp.Read(buf)
						if err != nil {
							panic(err)
						}
						slf.PushMessage(MessageTypePacket, conn, buf[:n])
					}
				}(conn)
			}
		})
	case NetworkHttp:
		if slf.prod {
			log.SetProd()
			gin.SetMode(gin.ReleaseMode)
		}
		go func() {
			slf.OnStartBeforeEvent()
			slf.httpServer.Addr = slf.addr
			if len(slf.certFile)+len(slf.keyFile) > 0 {
				if err := slf.httpServer.ListenAndServeTLS(slf.certFile, slf.keyFile); err != nil {
					slf.PushMessage(MessageTypeError, err, MessageErrorActionShutdown)
				}
			} else {
				if err := slf.httpServer.ListenAndServe(); err != nil {
					slf.PushMessage(MessageTypeError, err, MessageErrorActionShutdown)
				}
			}

		}()
	case NetworkWebsocket:
		go connectionInitHandle(func() {
			var pattern string
			var index = strings.Index(addr, "/")
			if index == -1 {
				pattern = "/"
			} else {
				pattern = addr[index:]
				slf.addr = slf.addr[:index]
			}
			var upgrade = websocket.Upgrader{
				ReadBufferSize:  4096,
				WriteBufferSize: 4096,
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}
			http.HandleFunc(pattern, func(writer http.ResponseWriter, request *http.Request) {
				ip := request.Header.Get("X-Real-IP")
				ws, err := upgrade.Upgrade(writer, request, nil)
				if err != nil {
					return
				}
				if len(ip) == 0 {
					addr := ws.RemoteAddr().String()
					if index := strings.LastIndex(addr, ":"); index != -1 {
						ip = addr[0:index]
					}
				}

				conn := newWebsocketConn(slf, ws, ip)
				for k, v := range request.URL.Query() {
					if len(v) == 1 {
						conn.SetData(k, v)
					} else {
						conn.SetData(k, v)
					}
				}
				slf.OnConnectionOpenedEvent(conn)

				defer func() {
					if err := recover(); err != nil {
						slf.OnConnectionClosedEvent(conn)
					}
				}()

				for {
					if err := ws.SetReadDeadline(time.Now().Add(time.Second * 30)); err != nil {
						panic(err)
					}
					messageType, packet, readErr := ws.ReadMessage()
					if readErr != nil {
						panic(readErr)
					}
					if len(slf.supportMessageTypes) > 0 && !slf.supportMessageTypes[messageType] {
						panic(ErrWebsocketIllegalMessageType)
					}
					slf.PushMessage(MessageTypePacket, conn, packet, messageType)
				}
			})
			go func() {
				slf.OnStartBeforeEvent()
				if len(slf.certFile)+len(slf.keyFile) > 0 {
					if err := http.ListenAndServeTLS(slf.addr, slf.certFile, slf.keyFile, nil); err != nil {
						slf.PushMessage(MessageTypeError, err, MessageErrorActionShutdown)
					}
				} else {
					if err := http.ListenAndServe(slf.addr, nil); err != nil {
						slf.PushMessage(MessageTypeError, err, MessageErrorActionShutdown)
					}
				}

			}()
		})
	default:
		return ErrCanNotSupportNetwork
	}

	if !slf.multiple {
		log.Info("Server", zap.String("Minotaur Server", "===================================================================="))
		log.Info("Server", zap.String("Minotaur Server", "RunningInfo"),
			zap.Any("network", slf.network),
			zap.String("listen", slf.addr),
		)
		log.Info("Server", zap.String("Minotaur Server", "===================================================================="))
		slf.OnStartFinishEvent()
		systemSignal := make(chan os.Signal, 1)
		signal.Notify(systemSignal, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
		select {
		case <-systemSignal:
			slf.Shutdown(nil)
		case <-slf.closeChannel:
			close(slf.closeChannel)
			break
		}
	} else {
		slf.OnStartFinishEvent()
	}

	return nil
}

// IsProd 是否为生产模式
func (slf *Server) IsProd() bool {
	return slf.prod
}

// IsDev 是否为开发模式
func (slf *Server) IsDev() bool {
	return !slf.prod
}

// GetID 获取服务器id
func (slf *Server) GetID() int64 {
	if slf.cross == nil {
		panic(ErrNoSupportCross)
	}
	return slf.id
}

// Ticker 获取服务器定时器
func (slf *Server) Ticker() *timer.Ticker {
	if slf.ticker == nil {
		panic(ErrNoSupportTicker)
	}
	return slf.ticker
}

// Shutdown 停止运行服务器
func (slf *Server) Shutdown(err error, stack ...string) {
	slf.isShutdown.Store(true)
	if slf.ticker != nil {
		slf.ticker.Release()
	}
	for _, cross := range slf.cross {
		cross.Release()
	}
	if slf.initMessageChannel {
		if slf.gServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if shutdownErr := gnet.Stop(ctx, fmt.Sprintf("%s://%s", slf.network, slf.addr)); shutdownErr != nil {
				log.Error("Server", zap.Error(shutdownErr))
			}
		}
		for _, messageChannel := range slf.messageChannel {
			close(messageChannel)
		}
		slf.messagePool.Close()
		slf.initMessageChannel = false
	}
	if slf.grpcServer != nil {
		slf.grpcServer.GracefulStop()
	}
	if slf.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if shutdownErr := slf.httpServer.Shutdown(ctx); shutdownErr != nil {
			log.Error("Server", zap.Error(shutdownErr))
		}
	}

	if err != nil {
		var s string
		if len(stack) > 0 {
			s = stack[0]
		}
		log.ErrorWithStack("Server", s, zap.Any("network", slf.network), zap.String("listen", slf.addr),
			zap.String("action", "shutdown"), zap.String("state", "exception"), zap.Error(err))
		slf.closeChannel <- struct{}{}
	} else {
		log.Info("Server", zap.Any("network", slf.network), zap.String("listen", slf.addr),
			zap.String("action", "shutdown"), zap.String("state", "normal"))
	}
}

func (slf *Server) GRPCServer() *grpc.Server {
	if slf.grpcServer == nil {
		panic(ErrNetworkOnlySupportGRPC)
	}
	return slf.grpcServer
}

// HttpRouter 当网络类型为 NetworkHttp 时将被允许获取路由器进行路由注册，否则将会发生 panic
func (slf *Server) HttpRouter() gin.IRouter {
	if slf.ginServer == nil {
		panic(ErrNetworkOnlySupportHttp)
	}
	return slf.ginServer
}

// PushMessage 向服务器中写入特定类型的消息，需严格遵守消息属性要求
func (slf *Server) PushMessage(messageType MessageType, attrs ...any) {
	msg := slf.messagePool.Get()
	msg.t = messageType
	msg.attrs = attrs
	if msg.t == MessageTypeError {
		msg.attrs = append(msg.attrs, string(debug.Stack()))
	}
	for _, channel := range slf.messageChannel {
		channel <- msg
		break
	}
}

// PushCrossMessage 推送跨服消息到特定跨服的服务器中
func (slf *Server) PushCrossMessage(crossName string, serverId int64, packet []byte) error {
	if len(slf.cross) == 0 {
		return ErrNoSupportCross
	}
	cross, exist := slf.cross[crossName]
	if !exist {
		return ErrUnregisteredCrossName
	}
	return cross.PushMessage(serverId, packet)
}

// dispatchMessage 消息分发
func (slf *Server) dispatchMessage(msg *Message) {
	present := time.Now()
	defer func() {
		if err := recover(); err != nil {
			log.Error("Server", zap.String("MessageType", messageNames[msg.t]), zap.Any("MessageAttrs", msg.attrs), zap.Any("error", err))
			if e, ok := err.(error); ok {
				slf.OnMessageErrorEvent(msg, e)
			}
		}

		if cost := time.Since(present); cost > time.Millisecond*100 {
			log.Warn("Server", zap.String("MessageType", messageNames[msg.t]), zap.String("LowExecCost", cost.String()), zap.Any("MessageAttrs", msg.attrs))
			slf.OnMessageLowExecEvent(msg, cost)
		}

		if !slf.isShutdown.Load() {
			slf.messagePool.Release(msg)
		}
	}()
	switch msg.t {
	case MessageTypePacket:
		if slf.network == NetworkWebsocket {
			conn, packet, messageType := msg.t.deconstructWebSocketPacket(msg.attrs...)
			slf.OnConnectionReceiveWebsocketPacketEvent(conn, packet, messageType)
		} else {
			conn, packet := msg.t.deconstructPacket(msg.attrs...)
			slf.OnConnectionReceivePacketEvent(conn, packet)
		}
	case MessageTypeError:
		err, action, stack := msg.t.deconstructError(msg.attrs...)
		switch action {
		case MessageErrorActionNone:
			log.ErrorWithStack("Server", stack, zap.Error(err))
		case MessageErrorActionShutdown:
			slf.Shutdown(err, stack)
		default:
			log.Warn("Server", zap.String("not support message error action", action.String()))
		}
	case MessageTypeCross:
		serverId, packet := msg.t.deconstructCross(msg.attrs...)
		slf.OnReceiveCrossPacketEvent(serverId, packet)
	case MessageTypeTicker:
		caller := msg.t.deconstructTicker(msg.attrs...)
		caller()
	default:
		log.Warn("Server", zap.String("not support message type", msg.t.String()))
	}
}
