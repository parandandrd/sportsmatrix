package imgcanvas

import (
	"net/http"

	"github.com/parandandrd/sportsmatrix/internal/board"
)

// GetHTTPHandlers ...
func (i *ImgCanvas) GetHTTPHandlers() ([]*board.HTTPHandler, error) {
	enable := &board.HTTPHandler{
		Path: "/api/imgcanvas/enable",
		Handler: func(w http.ResponseWriter, req *http.Request) {
			i.log.Info("enabling ImgCanvas")
			i.Enable()
		},
	}
	disable := &board.HTTPHandler{
		Path: "/api/imgcanvas/disable",
		Handler: func(w http.ResponseWriter, req *http.Request) {
			i.log.Info("disabling ImgCanvas")
			i.Disable()
		},
	}
	// Asking for a frame is what keeps the canvas drawing. It answers 204 No
	// Content until a board has drawn one; see FramePNG.
	render := &board.HTTPHandler{
		Path: "/api/imgcanvas/board",
		Handler: func(w http.ResponseWriter, req *http.Request) {
			if i.Enable() {
				i.log.Info("drawing the full-size web board from the next board on")
			}
			board.ServeFrame(w, req, i)
		},
	}

	return []*board.HTTPHandler{
		enable,
		disable,
		render,
	}, nil
}
