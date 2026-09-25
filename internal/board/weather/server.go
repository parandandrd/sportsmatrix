package weatherboard

import (
	"context"

	"github.com/twitchtv/twirp"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/parandandrd/sportsmatrix/internal/board"
	pb "github.com/parandandrd/sportsmatrix/internal/proto/basicboard"
)

// server is a weather board's own service, for switching it on and off.
type server struct {
	board board.Board
}

// SetStatus ...
func (s *server) SetStatus(ctx context.Context, req *pb.SetStatusReq) (*emptypb.Empty, error) {
	if req.Status == nil {
		return &emptypb.Empty{}, twirp.NewError(twirp.InvalidArgument, "nil status sent")
	}

	_ = s.board.Enabler().Store(req.Status.Enabled)

	return &emptypb.Empty{}, nil
}

// GetStatus ...
func (s *server) GetStatus(ctx context.Context, req *emptypb.Empty) (*pb.StatusResp, error) {
	return &pb.StatusResp{
		Status: &pb.Status{
			Enabled: s.board.Enabler().Enabled(),
		},
	}, nil
}
