package sportsmatrix

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/twitchtv/twirp"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/parandandrd/sportsmatrix/internal/board"
	sportboard "github.com/parandandrd/sportsmatrix/internal/board/sport"
	pb "github.com/parandandrd/sportsmatrix/internal/proto/sportsmatrix"
)

// Server ...
type Server struct {
	sm *SportsMatrix
}

// Version ...
func (s *Server) Version(ctx context.Context, req *emptypb.Empty) (*pb.VersionResp, error) {
	return &pb.VersionResp{
		Version: version,
	}, nil
}

// ScreenOn ...
func (s *Server) ScreenOn(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.sm.ScreenOn(ctx); err != nil {
		return &emptypb.Empty{}, twirp.NewError(twirp.Internal, "failed to turn screen on")
	}
	return &emptypb.Empty{}, nil
}

// ScreenOff ...
func (s *Server) ScreenOff(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.sm.ScreenOff(ctx); err != nil {
		return &emptypb.Empty{}, twirp.NewError(twirp.Internal, "failed to turn screen off")
	}
	return &emptypb.Empty{}, nil
}

// ListBoards returns the boards this instance was configured with, and whether
// each is currently enabled. Boards are only constructed when their config
// section is present, so this is what actually exists rather than everything
// the binary can render.
func (s *Server) ListBoards(ctx context.Context, req *emptypb.Empty) (*pb.ListBoardsResp, error) {
	s.sm.Lock()
	defer s.sm.Unlock()

	resp := &pb.ListBoardsResp{
		Boards: make([]*pb.BoardInfo, 0, len(s.sm.boards)+len(s.sm.betweenBoards)),
	}

	info := func(b board.Board, inBetween bool) *pb.BoardInfo {
		// the path a board mounts its own service on is the only thing that
		// says what kind of board it is; Name is a display name, and for the
		// sport boards it is the league's full name rather than its slug.
		path, h := b.GetRPCHandler()
		if h == nil {
			path = ""
		}

		return &pb.BoardInfo{
			Name:      b.Name(),
			Enabled:   b.Enabler().Enabled(),
			InBetween: inBetween,
			RpcPath:   path,
		}
	}

	for _, b := range s.sm.boards {
		resp.Boards = append(resp.Boards, info(b, false))
	}

	for _, b := range s.sm.betweenBoards {
		resp.Boards = append(resp.Boards, info(b, true))
	}

	return resp, nil
}

// SetBoardEnabled turns a single board on or off by the name ListBoards
// reports. SetAll is all or nothing, and every other route to one board's
// enabled state runs through that board's own service, so a caller has to know
// the board's kind and RPC path before it can flip a switch. Matching is
// case-insensitive, like Jump.
func (s *Server) SetBoardEnabled(ctx context.Context, req *pb.SetBoardEnabledReq) (*emptypb.Empty, error) {
	s.sm.Lock()
	defer s.sm.Unlock()

	found := false

	for _, group := range [][]board.Board{s.sm.boards, s.sm.betweenBoards} {
		for _, b := range group {
			if !strings.EqualFold(b.Name(), req.Name) {
				continue
			}
			found = true
			if req.Enabled {
				b.Enabler().Enable()
			} else {
				b.Enabler().Disable()
			}
		}
	}

	if !found {
		return nil, twirp.NewError(twirp.NotFound, fmt.Sprintf("no board named %q", req.Name))
	}

	return &emptypb.Empty{}, nil
}

// SetAll ...
func (s *Server) SetAll(ctx context.Context, req *pb.SetAllReq) (*emptypb.Empty, error) {
	s.sm.Lock()
	defer s.sm.Unlock()

	if req.Enabled {
		for _, board := range s.sm.boards {
			board.Enabler().Enable()
		}
		for _, board := range s.sm.betweenBoards {
			board.Enabler().Enable()
		}
	} else {
		for _, board := range s.sm.boards {
			board.Enabler().Disable()
		}
		for _, board := range s.sm.betweenBoards {
			board.Enabler().Disable()
		}
	}

	return &emptypb.Empty{}, nil
}

// Jump ...
func (s *Server) Jump(ctx context.Context, req *pb.JumpReq) (*emptypb.Empty, error) {
	if s.sm.jumping.Load() {
		return &emptypb.Empty{}, nil
	}

	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.sm.JumpTo(c, req.Board); err != nil {
		return nil, twirp.NewError(twirp.Internal, err.Error())
	}

	return &emptypb.Empty{}, nil
}

// SetStatus ...
func (s *Server) SetStatus(ctx context.Context, req *pb.Status) (*emptypb.Empty, error) {
	if req.ScreenOn {
		if _, err := s.ScreenOn(ctx, &emptypb.Empty{}); err != nil {
			return nil, twirp.NewError(twirp.Internal, err.Error())
		}
	} else {
		if _, err := s.ScreenOff(ctx, &emptypb.Empty{}); err != nil {
			return nil, twirp.NewError(twirp.Internal, err.Error())
		}
	}

	if req.WebboardOn {
		if !s.sm.webBoardIsOn.Load() {
			s.sm.startWebBoard(ctx)
		}
	} else {
		if s.sm.webBoardIsOn.Load() {
			s.sm.stopWebBoard()
		}
	}

	return &emptypb.Empty{}, nil
}

// GetStatus ...
func (s *Server) GetStatus(ctx context.Context, req *emptypb.Empty) (*pb.Status, error) {
	return &pb.Status{
		ScreenOn:   s.sm.screenIsOn.Load(),
		WebboardOn: s.sm.webBoardIsOn.Load(),
	}, nil
}

// NextBoard jumps to the next board in the sequence
func (s *Server) NextBoard(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	s.sm.Lock()
	defer s.sm.Unlock()
	s.sm.currentBoardCancel()
	return &emptypb.Empty{}, nil
}

// RestartService restarts the sportsmatrix service
func (s *Server) RestartService(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	myPid := os.Getpid()
	s.sm.log.Warn("restarting sportsmatrix service",
		zap.Int("pid", myPid),
	)

	proc, err := os.FindProcess(myPid)
	if err != nil {
		return nil, twirp.NewError(twirp.Internal, err.Error())
	}

	go func() {
		time.Sleep(2 * time.Second)
		if err := proc.Signal(syscall.SIGHUP); err != nil {
			s.sm.log.Error("failed to restart service",
				zap.Error(err),
			)
		}
	}()

	return &emptypb.Empty{}, nil
}

// SetLiveOnly sets the LiveOnly setting for SportBoards
func (s *Server) SetLiveOnly(ctx context.Context, req *pb.LiveOnlyReq) (*emptypb.Empty, error) {
	for _, board := range s.sm.boards {
		if sportBoard, ok := board.(*sportboard.SportBoard); ok {
			sportBoard.SetLiveOnly(req.LiveOnly)
		}
	}

	return &emptypb.Empty{}, nil
}
