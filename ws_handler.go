package videoserver

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/morozka/vdk/format/mp4f"
)

func wshandler(wsUpgrader *websocket.Upgrader, w http.ResponseWriter, r *http.Request, app *Application) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to make websocket upgrade: %s\n", err.Error())
		return
	}
	defer func() {
		err = conn.Close()
		if err != nil {
			// log.Printf("WS connection has been closed %s: %s\n", conn.RemoteAddr().String(), err.Error())
		}
		// log.Printf("WS connection has been terminated %s\n", conn.RemoteAddr().String())
	}()

	streamIDSTR := r.FormValue("suuid")
	streamID, err := uuid.Parse(streamIDSTR)
	if err != nil {
		log.Printf("Can't parse UUID: '%s' due the error: %s\n", streamIDSTR, err.Error())
		return
	}

	if app.existsWithType(streamID, "mse") {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		cuuid, ch, err := app.clientAdd(streamID)
		if err != nil {
			log.Printf("Can't add client for '%s' due the error: %s\n", streamID, err.Error())
			return
		}
		defer app.clientDelete(streamID, cuuid)
		codecData, err := app.codecGet(streamID)
		if err != nil {
			log.Printf("Can't add client '%s' due the error: %s\n", streamID, err.Error())
			return
		}
		if codecData == nil {
			log.Printf("No codec information for stream %s\n", streamID)
			return
		}
		muxer := mp4f.NewMuxer(nil)
		muxer.WriteHeader(codecData)
		meta, init := muxer.GetInit(codecData)
		err = conn.WriteMessage(websocket.BinaryMessage, append([]byte{9}, meta...))
		if err != nil {
			log.Printf("Can't write header to %s: %s\n", conn.RemoteAddr().String(), err.Error())
			return
		}
		err = conn.WriteMessage(websocket.BinaryMessage, init)
		if err != nil {
			log.Printf("Can't write message to %s: %s\n", conn.RemoteAddr().String(), err.Error())
			return
		}
		var start bool
		quitCh := make(chan bool)
		go func(q chan bool) {
			_, _, err := conn.ReadMessage()
			if err != nil {
				q <- true
				log.Printf("Read message error: %s\n", err.Error())
				return
			}
		}(quitCh)
		for {
			select {
			case <-quitCh:
				return
			case pck := <-ch:
				if pck.IsKeyFrame {
					start = true
				}
				if !start {
					continue
				}
				ready, buf, _ := muxer.WritePacket(pck, false)
				if ready {
					conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					err := conn.WriteMessage(websocket.BinaryMessage, buf)
					if err != nil {
						log.Printf("Can't write messsage due the error: %s\n", err.Error())
						return
					}
				}
			}
		}
	}
}
