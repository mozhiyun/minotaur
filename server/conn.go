package server

import (
	"github.com/gorilla/websocket"
	"github.com/panjf2000/gnet"
	"github.com/xtaci/kcp-go/v5"
)

// newKcpConn 创建一个处理KCP的连接
func newKcpConn(session *kcp.UDPSession) *Conn {
	return &Conn{
		ip:  session.RemoteAddr().String(),
		kcp: session,
		write: func(data []byte) error {
			_, err := session.Write(data)
			return err
		},
		data: map[any]any{},
	}
}

// newKcpConn 创建一个处理GNet的连接
func newGNetConn(conn gnet.Conn) *Conn {
	return &Conn{
		ip: conn.RemoteAddr().String(),
		gn: conn,
		write: func(data []byte) error {
			return conn.AsyncWrite(data)
		},
		data: map[any]any{},
	}
}

// newKcpConn 创建一个处理WebSocket的连接
func newWebsocketConn(ws *websocket.Conn, ip string) *Conn {
	return &Conn{
		ip: ip,
		ws: ws,
		write: func(data []byte) error {
			return ws.WriteMessage(websocket.BinaryMessage, data)
		},
		data: map[any]any{},
	}
}

// Conn 服务器连接
type Conn struct {
	ip    string
	ws    *websocket.Conn
	gn    gnet.Conn
	kcp   *kcp.UDPSession
	write func(data []byte) error
	data  map[any]any
}

func (slf *Conn) GetID() string {
	return slf.ip
}

// Write 向连接中写入数据
func (slf *Conn) Write(data []byte) error {
	return slf.write(data)
}

// Close 关闭连接
func (slf *Conn) Close() {
	if slf.ws != nil {
		_ = slf.ws.Close()
	} else if slf.gn != nil {
		_ = slf.gn.Close()
	} else if slf.kcp != nil {
		_ = slf.kcp.Close()
	}
	slf.write = nil
}

// SetData 设置连接数据
func (slf *Conn) SetData(key, value any) *Conn {
	slf.data[key] = value
	return slf
}

// GetData 获取连接数据
func (slf *Conn) GetData(key any) any {
	return slf.data[key]
}

// ReleaseData 释放数据
func (slf *Conn) ReleaseData() *Conn {
	for k := range slf.data {
		delete(slf.data, k)
	}
	return slf
}
