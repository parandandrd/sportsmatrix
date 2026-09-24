package sportsmatrix

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/twitchtv/twirp"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/parandandrd/sportsmatrix/internal/board"
	sportboard "github.com/parandandrd/sportsmatrix/internal/board/sport"
	"github.com/parandandrd/sportsmatrix/internal/conffile"
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

// ListBoards returns every board this instance runs and whether each is
// currently enabled, in the order of the config file sections they come from.
// Every board the binary can render is built whether or not the config file
// mentions it, so each also says whether its section is really in the file.
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

		section := s.sm.boardSections[b]
		_, inFile := s.sm.sectionOrder[strings.ToLower(section)]

		return &pb.BoardInfo{
			Name:         b.Name(),
			Enabled:      b.Enabler().Enabled(),
			InBetween:    inBetween,
			RpcPath:      path,
			Section:      section,
			InConfigFile: section != "" && inFile,
		}
	}

	for _, b := range s.sm.boards {
		resp.Boards = append(resp.Boards, info(b, false))
	}

	for _, b := range s.sm.betweenBoards {
		resp.Boards = append(resp.Boards, info(b, true))
	}

	// A section the file leaves out goes last. The sort is stable, so boards
	// built from one section stay together and in the order they were built,
	// which puts a league's own board ahead of its stats and headlines.
	position := func(bi *pb.BoardInfo) int {
		if i, ok := s.sm.sectionOrder[strings.ToLower(bi.Section)]; ok && bi.InConfigFile {
			return i
		}
		return len(s.sm.sectionOrder)
	}
	sort.SliceStable(resp.Boards, func(i, j int) bool {
		return position(resp.Boards[i]) < position(resp.Boards[j])
	})

	return resp, nil
}

// SetBoardEnabled turns a single board on or off by the name ListBoards
// reports. SetAll is all or nothing, and every other route to one board's
// enabled state runs through that board's own service, so a caller has to know
// the board's kind and RPC path before it can flip a switch. Matching is
// case-insensitive, like Jump.
func (s *Server) SetBoardEnabled(ctx context.Context, req *pb.SetBoardEnabledReq) (*emptypb.Empty, error) {
	var matched []board.Board

	s.sm.Lock()
	for _, group := range [][]board.Board{s.sm.boards, s.sm.betweenBoards} {
		for _, b := range group {
			if strings.EqualFold(b.Name(), req.Name) {
				matched = append(matched, b)
			}
		}
	}
	s.sm.Unlock()

	if len(matched) == 0 {
		return nil, twirp.NewError(twirp.NotFound, fmt.Sprintf("no board named %q", req.Name))
	}

	if err := s.sm.setEnabled(matched, req.Enabled); err != nil {
		return nil, twirp.NewError(twirp.Internal, err.Error())
	}

	return &emptypb.Empty{}, nil
}

// SetBoardOrder rearranges boards by the config sections they come from.
func (s *Server) SetBoardOrder(ctx context.Context, req *pb.SetBoardOrderReq) (*emptypb.Empty, error) {
	if err := s.sm.setBoardOrder(req.Sections); err != nil {
		return nil, settingsError(err)
	}
	return &emptypb.Empty{}, nil
}

// GetSettings reports the matrix-wide settings.
func (s *Server) GetSettings(ctx context.Context, req *emptypb.Empty) (*pb.Settings, error) {
	return s.sm.settings(), nil
}

// SetBrightness changes the panel's brightness now, and saves it.
func (s *Server) SetBrightness(ctx context.Context, req *pb.SetBrightnessReq) (*emptypb.Empty, error) {
	if err := s.sm.setBrightness(int(req.Brightness)); err != nil {
		return nil, settingsError(err)
	}
	return &emptypb.Empty{}, nil
}

// SetScreenSchedule replaces when the screen turns itself on and off, and
// saves it.
func (s *Server) SetScreenSchedule(ctx context.Context, req *pb.ScreenSchedule) (*emptypb.Empty, error) {
	if err := s.sm.setScreenSchedule(req.OnTimes, req.OffTimes); err != nil {
		return nil, settingsError(err)
	}
	return &emptypb.Empty{}, nil
}

// GetBoardSettings reports one board's settings, and the choices for them.
func (s *Server) GetBoardSettings(ctx context.Context, req *pb.BoardSettingsReq) (*pb.BoardSettings, error) {
	out, err := s.sm.boardSettings(ctx, req.Name)
	if err != nil {
		return nil, settingsError(err)
	}
	return out, nil
}

// SetBoardSettings changes one board's settings now, and saves them.
func (s *Server) SetBoardSettings(ctx context.Context, req *pb.BoardSettings) (*emptypb.Empty, error) {
	if err := s.sm.setBoardSettings(req); err != nil {
		return nil, settingsError(err)
	}
	return &emptypb.Empty{}, nil
}

func settingsError(err error) error {
	switch {
	case errors.Is(err, errUnknownBoard):
		return twirp.NewError(twirp.NotFound, err.Error())
	case errors.Is(err, errBadBrightness), errors.Is(err, errBadSchedule), errors.Is(err, errUnknownSection),
		errors.Is(err, errNoSuchSetting), errors.Is(err, errBadDelay):
		return twirp.NewError(twirp.InvalidArgument, err.Error())
	case errors.Is(err, errNoConfigFile):
		return twirp.NewError(twirp.FailedPrecondition, err.Error())
	default:
		return twirp.NewError(twirp.Internal, err.Error())
	}
}

// SetAll ...
func (s *Server) SetAll(ctx context.Context, req *pb.SetAllReq) (*emptypb.Empty, error) {
	s.sm.Lock()
	all := append(append([]board.Board(nil), s.sm.boards...), s.sm.betweenBoards...)
	s.sm.Unlock()

	if err := s.sm.setEnabled(all, req.Enabled); err != nil {
		return nil, twirp.NewError(twirp.Internal, err.Error())
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
	// ScreenOn and ScreenOff already answer with twirp errors
	if req.ScreenOn {
		if _, err := s.ScreenOn(ctx, &emptypb.Empty{}); err != nil {
			return nil, err
		}
	} else {
		if _, err := s.ScreenOff(ctx, &emptypb.Empty{}); err != nil {
			return nil, err
		}
	}

	return &emptypb.Empty{}, nil
}

// GetStatus ...
func (s *Server) GetStatus(ctx context.Context, req *emptypb.Empty) (*pb.Status, error) {
	return &pb.Status{
		ScreenOn: s.sm.screenIsOn.Load(),
	}, nil
}

// NextBoard jumps to the next board in the sequence
func (s *Server) NextBoard(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	s.sm.nextBoard()
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
	s.sm.Lock()
	boards := append([]board.Board(nil), s.sm.boards...)
	s.sm.Unlock()

	s.sm.settingsLock.Lock()
	defer s.sm.settingsLock.Unlock()

	var edits []conffile.Edit
	for _, b := range boards {
		if sportBoard, ok := b.(*sportboard.SportBoard); ok && sportBoard.SetLiveOnly(req.LiveOnly) {
			if e := s.sm.boardEdit(b, "liveOnly", req.LiveOnly); e != nil {
				edits = append(edits, *e)
			}
		}
	}

	if err := s.sm.save(edits...); err != nil {
		return nil, twirp.NewError(twirp.Internal, err.Error())
	}

	return &emptypb.Empty{}, nil
}
