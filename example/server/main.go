package main

import (
	"context"
	"image"
	"log"
	"math"
	"net"
	"time"

	vnc "github.com/unistack-org/go-rfb"
)

func main() {
	ln, err := net.Listen("tcp", ":6900")
	if err != nil {
		log.Fatalf("Error listen. %v", err)
	}

	chServer := make(chan vnc.ClientMessage)
	chClient := make(chan vnc.ServerMessage)

	im := image.NewRGBA(image.Rect(0, 0, width, height))
	tick := time.NewTicker(time.Millisecond * 2)
	defer tick.Stop()
	connected := false

	cfg := &vnc.ServerConfig{
		Width:            width,
		Height:           height,
		Handlers:         vnc.DefaultServerHandlers,
		SecurityHandlers: []vnc.SecurityHandler{&vnc.ClientAuthNone{}},
		Encodings:        []vnc.Encoding{&vnc.RawEncoding{}},
		PixelFormat:      vnc.PixelFormat32bit,
		ClientMessageCh:  chServer,
		ServerMessageCh:  chClient,
		Messages:         vnc.DefaultClientMessages,
	}
	go vnc.Serve(context.Background(), ln, cfg)
	anim := 0
	// Process messages coming in on the ClientMessage channel.
	for {
		select {
		case <-tick.C:
			if !connected {
				continue
			}
			drawImage(im, anim*2)
			anim++
			colors := make([]vnc.Color, 0, 0)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					r, g, b, a := im.At(x, y).RGBA()
					clr := rgbaToColor(cfg.PixelFormat, r, g, b, a)
					colors = append(colors, *clr)
				}
			}
			cfg.ServerMessageCh <- &vnc.FramebufferUpdate{
				NumRect: 1,
				Rects: []*vnc.Rectangle{
					&vnc.Rectangle{
						X:       0,
						Y:       0,
						Width:   width,
						Height:  height,
						EncType: vnc.EncRaw,
						Enc: &vnc.RawEncoding{
							Colors: colors,
						},
					}}}
			/*
				case msg := <-chClient:
					switch msg.Type() {
					case vnc.FramebufferUpdateMsgType:
						connected = true
						log.Printf("11 Received message type:%v msg:%v\n", msg.Type(), msg)
					default:
						log.Printf("11 Received message type:%v msg:%v\n", msg.Type(), msg)
					}*/
		case msg := <-chServer:
			switch msg.Type() {
			case vnc.FramebufferUpdateRequestMsgType:
				connected = true
			default:
				log.Printf("22 Received message type:%v msg:%v\n", msg.Type(), msg)
			}

		}
	}
}

const (
	width  = 800
	height = 600
)

func rgbaToColor(pf *vnc.PixelFormat, r uint32, g uint32, b uint32, a uint32) *vnc.Color {
	// fix converting rbga to rgb http://marcodiiga.github.io/rgba-to-rgb-conversion
	clr := vnc.NewColor(pf, nil)
	clr.R = uint16(r / 257)
	clr.G = uint16(g / 257)
	clr.B = uint16(b / 257)
	return clr
}

func drawImage(im *image.RGBA, anim int) {
	pos := 0
	const border = 50
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var r, g, b uint8
			switch {
			case x < border*2.5 && x < int((1.1+math.Sin(float64(y+anim*2)/40))*border):
				r = 255
			case x > width-border*2.5 && x > width-int((1.1+math.Sin(math.Pi+float64(y+anim*2)/40))*border):
				g = 255
			case y < border*2.5 && y < int((1.1+math.Sin(float64(x+anim*2)/40))*border):
				r, g = 255, 255
			case y > height-border*2.5 && y > height-int((1.1+math.Sin(math.Pi+float64(x+anim*2)/40))*border):
				b = 255
			default:
				r, g, b = uint8(x+anim), uint8(y+anim), uint8(x+y+anim*3)
			}
			im.Pix[pos] = r
			im.Pix[pos+1] = g
			im.Pix[pos+2] = b
			pos += 4 // skipping alpha
		}
	}
}
