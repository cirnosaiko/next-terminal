package api

import (
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"next-terminal/pkg/global"
	"next-terminal/pkg/guacd"
	"next-terminal/pkg/model"
	"path"
	"strconv"
)

const (
	Normal             int = 0
	NotFoundSession    int = 2000
	NewTunnelError     int = 2001
	NewSftpClientError int = 2002
	ForcedDisconnect   int = 2003
)

func TunEndpoint(c echo.Context) error {

	ws, err := UpGrader.Upgrade(c.Response().Writer, c.Request(), nil)
	if err != nil {
		logrus.Errorf("升级为WebSocket协议失败：%v", err.Error())
		return err
	}

	width := c.QueryParam("width")
	height := c.QueryParam("height")
	sessionId := c.QueryParam("sessionId")
	connectionId := c.QueryParam("connectionId")

	intWidth, _ := strconv.Atoi(width)
	intHeight, _ := strconv.Atoi(height)

	configuration := guacd.NewConfiguration()
	configuration.SetParameter("width", width)
	configuration.SetParameter("height", height)

	propertyMap := model.FindAllPropertiesMap()

	var session model.Session

	if len(connectionId) > 0 {
		session, err = model.FindSessionByConnectionId(connectionId)
		if err != nil {
			CloseSessionById(sessionId, NotFoundSession, "会话不存在")
			return err
		}
		configuration.ConnectionID = connectionId
	} else {
		session, err = model.FindSessionById(sessionId)
		if err != nil {
			CloseSessionById(sessionId, NotFoundSession, "会话不存在")
			return err
		}

		if propertyMap[guacd.EnableRecording] == "true" {
			configuration.SetParameter(guacd.RecordingPath, path.Join(propertyMap[guacd.RecordingPath], sessionId))
			configuration.SetParameter(guacd.CreateRecordingPath, propertyMap[guacd.CreateRecordingPath])
		} else {
			configuration.SetParameter(guacd.RecordingPath, "")
		}

		configuration.Protocol = session.Protocol
		switch configuration.Protocol {
		case "rdp":
			configuration.SetParameter("username", session.Username)
			configuration.SetParameter("password", session.Password)

			configuration.SetParameter("security", "any")
			configuration.SetParameter("ignore-cert", "true")
			configuration.SetParameter("create-drive-path", "true")

			configuration.SetParameter("dpi", "96")
			configuration.SetParameter("resize-method", "reconnect")
			configuration.SetParameter(guacd.EnableDrive, propertyMap[guacd.EnableDrive])
			configuration.SetParameter(guacd.DriveName, propertyMap[guacd.DriveName])
			configuration.SetParameter(guacd.DrivePath, propertyMap[guacd.DrivePath])
			configuration.SetParameter(guacd.EnableWallpaper, propertyMap[guacd.EnableWallpaper])
			configuration.SetParameter(guacd.EnableTheming, propertyMap[guacd.EnableTheming])
			configuration.SetParameter(guacd.EnableFontSmoothing, propertyMap[guacd.EnableFontSmoothing])
			configuration.SetParameter(guacd.EnableFullWindowDrag, propertyMap[guacd.EnableFullWindowDrag])
			configuration.SetParameter(guacd.EnableDesktopComposition, propertyMap[guacd.EnableDesktopComposition])
			configuration.SetParameter(guacd.EnableMenuAnimations, propertyMap[guacd.EnableMenuAnimations])
			configuration.SetParameter(guacd.DisableBitmapCaching, propertyMap[guacd.DisableBitmapCaching])
			configuration.SetParameter(guacd.DisableOffscreenCaching, propertyMap[guacd.DisableOffscreenCaching])
			configuration.SetParameter(guacd.DisableGlyphCaching, propertyMap[guacd.DisableGlyphCaching])
			break
		case "ssh":
			if len(session.PrivateKey) > 0 && session.PrivateKey != "-" {
				configuration.SetParameter("username", session.Username)
				configuration.SetParameter("private-key", session.PrivateKey)
				configuration.SetParameter("passphrase", session.Passphrase)
			} else {
				configuration.SetParameter("username", session.Username)
				configuration.SetParameter("password", session.Password)
			}

			fontSize, _ := strconv.Atoi(propertyMap[guacd.FontSize])
			fontSize = fontSize * 2
			configuration.SetParameter(guacd.FontSize, strconv.Itoa(fontSize))
			configuration.SetParameter(guacd.FontName, propertyMap[guacd.FontName])
			configuration.SetParameter(guacd.ColorScheme, propertyMap[guacd.ColorScheme])
			break
		case "vnc":
			configuration.SetParameter("username", session.Username)
			configuration.SetParameter("password", session.Password)
			break
		case "telnet":
			configuration.SetParameter("username", session.Username)
			configuration.SetParameter("password", session.Password)
			break
		}

		configuration.SetParameter("hostname", session.IP)
		configuration.SetParameter("port", strconv.Itoa(session.Port))
	}

	addr := propertyMap[guacd.Host] + ":" + propertyMap[guacd.Port]

	logrus.Infof("connect to %v with global: %+v", addr, configuration)

	tunnel, err := guacd.NewTunnel(addr, configuration)
	if err != nil {
		CloseSessionById(sessionId, NewTunnelError, err.Error())
		logrus.Printf("建立连接失败: %v", err.Error())
		return err
	}

	tun := global.Tun{
		Tun:       tunnel,
		WebSocket: ws,
	}

	global.Store.Set(sessionId, &tun)

	if len(session.ConnectionId) == 0 {
		session.ConnectionId = tunnel.UUID
		session.Width = intWidth
		session.Height = intHeight
		session.Status = model.Connecting
		session.Recording = configuration.GetParameter(guacd.RecordingPath)

		model.UpdateSessionById(&session, sessionId)
	}

	go func() {
		for true {
			instruction, err := tunnel.Read()
			if err != nil {
				CloseSessionById(sessionId, Normal, "")
				break
			}
			err = ws.WriteMessage(websocket.TextMessage, instruction)
			if err != nil {
				CloseSessionById(sessionId, Normal, "")
				break
			}
		}
	}()

	for true {
		_, message, err := ws.ReadMessage()
		if err != nil {
			CloseSessionById(sessionId, Normal, "")
			break
		}
		_, err = tunnel.WriteAndFlush(message)
		if err != nil {
			CloseSessionById(sessionId, Normal, "")
			break
		}
	}
	return err
}
