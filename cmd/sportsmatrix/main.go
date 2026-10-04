package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"time"

	yaml "github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/board/clock"
	imageboard "github.com/parandandrd/sportsmatrix/internal/board/image"
	racingboard "github.com/parandandrd/sportsmatrix/internal/board/racing"
	sportboard "github.com/parandandrd/sportsmatrix/internal/board/sport"
	sysboard "github.com/parandandrd/sportsmatrix/internal/board/sys"
	textboard "github.com/parandandrd/sportsmatrix/internal/board/text"
	tvboard "github.com/parandandrd/sportsmatrix/internal/board/tv"
	weatherboard "github.com/parandandrd/sportsmatrix/internal/board/weather"
	"github.com/parandandrd/sportsmatrix/internal/config"
	"github.com/parandandrd/sportsmatrix/internal/espnboard"
	"github.com/parandandrd/sportsmatrix/internal/espnracing"
	"github.com/parandandrd/sportsmatrix/internal/logo"
	"github.com/parandandrd/sportsmatrix/internal/matrix"
	"github.com/parandandrd/sportsmatrix/internal/mlblive"
	rgb "github.com/parandandrd/sportsmatrix/internal/rgbmatrix-rpi"
	"github.com/parandandrd/sportsmatrix/internal/sportsmatrix"
)

const defaultConfigFile = "/etc/sportsmatrix.conf"

type rootArgs struct {
	level          string
	logLevel       zapcore.Level
	configFile     string
	config         *config.Config
	configPath     string
	configSections []string
	boardSections  map[board.Board]string
	test           bool
	today          string
	logFile        string
	writer         *os.File
	debug          bool
	todayT         *time.Time
}

func main() {
	args := &rootArgs{}

	rootCmd := newRootCmd(args)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err.Error())

		os.Exit(1)
	}

	os.Exit(0)
}

func newRootCmd(args *rootArgs) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "sports",
		Short: "Sports info",
		PersistentPreRunE: func(cmd *cobra.Command, a []string) error {
			configFile := viper.GetString("config")

			if configFile == defaultConfigFile {
				if _, err := os.Stat(configFile); err != nil && os.IsNotExist(err) {
					fmt.Println("Using default config")
					args.config = &config.Config{}
				} else {
					fmt.Printf("Loading config from file %s\n", configFile)
					if err := args.setConfig(configFile); err != nil {
						return fmt.Errorf("failed to load config file: %w", err)
					}
				}
			} else if configFile != "" {
				fmt.Printf("Loading config from file %s\n", configFile)
				if err := args.setConfig(configFile); err != nil {
					return fmt.Errorf("failed to load config file: %w", err)
				}
			} else {
				fmt.Println("Using default config")
				args.config = &config.Config{}
			}

			lvl := viper.GetString("log-level")

			if lvl == "" {
				args.logLevel = zapcore.InfoLevel
			} else {
				var l zapcore.Level
				if err := l.Set(lvl); err != nil {
					return err
				}
				args.logLevel = l
			}

			args.setConfigDefaults()

			if viper.GetBool("debug") || args.config.Debug {
				debugServer := NewDebugServer("0.0.0.0:6060")

				fmt.Println("Debug server running on port 6060")

				go func() {
					_ = debugServer.ListenAndServe()
				}()
			}

			if today := viper.GetString("date-str"); today != "" {
				var err error
				t, err := time.Parse("2006-01-02T15:04:05", fmt.Sprintf("%sT12:00:00", today))
				if err != nil {
					return fmt.Errorf("failed to parse date-str: %w", err)
				}
				args.todayT = &t
			} else {
				args.todayT = nil
			}

			return nil
		},
	}

	f := rootCmd.PersistentFlags()

	f.StringVarP(&args.configFile, "config", "c", defaultConfigFile, "Config filename")
	f.StringVarP(&args.level, "log-level", "l", "info", "Log level. 'info', 'warn', 'debug'")
	f.BoolVarP(&args.test, "test", "t", false, "uses a test console matrix")
	f.StringVar(&args.today, "date-str", "", "Set the date of 'Today' for testing past days. Format 2020-01-30")
	f.StringVarP(&args.logFile, "log-file", "f", "", "Write logs to given file instead of STDOUT")
	f.BoolVarP(&args.debug, "debug", "d", false, "Run pprof debug server on :6060")

	_ = viper.BindPFlags(f)

	rootCmd.AddCommand(newMlbCmd(args))
	rootCmd.AddCommand(newRunCmd(args))
	rootCmd.AddCommand(newNcaaMCmd(args))
	rootCmd.AddCommand(newAbbrevCmd(args))

	return rootCmd
}

func (r *rootArgs) setConfig(filename string) error {
	f, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	var c *config.Config

	if err := yaml.Unmarshal(f, &c); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	r.config = c
	r.configPath = filename

	// Only the web UI's board order depends on this, so a file that parsed
	// above is not rejected over it.
	r.configSections, _ = configSections(f)

	return nil
}

func (r *rootArgs) setConfigDefaults() {
	if r.config.SportsMatrixConfig == nil {
		r.config.SportsMatrixConfig = &sportsmatrix.Config{}
	}
	r.config.SportsMatrixConfig.Defaults()

	if r.config.NHLConfig == nil {
		r.config.NHLConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.NHLConfig.Headlines == nil {
		r.config.NHLConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}

	r.config.NHLConfig.SetDefaults()
	r.config.NHLConfig.Headlines.SetDefaults()

	if r.config.ImageConfig == nil {
		r.config.ImageConfig = &imageboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.ImageConfig.SetDefaults()

	if r.config.ClockConfig == nil {
		r.config.ClockConfig = &clock.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.ClockConfig.SetDefaults()

	if r.config.MLBConfig == nil {
		r.config.MLBConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.MLBConfig.Headlines == nil {
		r.config.MLBConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.MLBConfig.SetDefaults()
	r.config.MLBConfig.Headlines.SetDefaults()

	if r.config.NCAAMConfig == nil {
		r.config.NCAAMConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.NCAAMConfig.Headlines == nil {
		r.config.NCAAMConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.NCAAMConfig.SetDefaults()
	r.config.NCAAMConfig.Headlines.SetDefaults()

	if r.config.NCAAFConfig == nil {
		r.config.NCAAFConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.NCAAFConfig.Headlines == nil {
		r.config.NCAAFConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.NCAAFConfig.SetDefaults()
	r.config.NCAAFConfig.Headlines.SetDefaults()

	if r.config.NBAConfig == nil {
		r.config.NBAConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.NBAConfig.Headlines == nil {
		r.config.NBAConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.NBAConfig.SetDefaults()
	r.config.NBAConfig.Headlines.SetDefaults()

	if r.config.NFLConfig == nil {
		r.config.NFLConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.NFLConfig.Headlines == nil {
		r.config.NFLConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.NFLConfig.SetDefaults()
	r.config.NFLConfig.Headlines.SetDefaults()

	if r.config.MLSConfig == nil {
		r.config.MLSConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.MLSConfig.Headlines == nil {
		r.config.MLSConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.MLSConfig.SetDefaults()
	r.config.MLSConfig.Headlines.SetDefaults()

	if r.config.NWSLConfig == nil {
		r.config.NWSLConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.NWSLConfig.Headlines == nil {
		r.config.NWSLConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.NWSLConfig.SetDefaults()
	r.config.NWSLConfig.Headlines.SetDefaults()

	if r.config.EPLConfig == nil {
		r.config.EPLConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.EPLConfig.Headlines == nil {
		r.config.EPLConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.EPLConfig.SetDefaults()
	r.config.EPLConfig.Headlines.SetDefaults()

	if r.config.DFLConfig == nil {
		r.config.DFLConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.DFLConfig.Headlines == nil {
		r.config.DFLConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.DFLConfig.SetDefaults()
	r.config.DFLConfig.Headlines.SetDefaults()

	if r.config.DFBConfig == nil {
		r.config.DFBConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.DFBConfig.Headlines == nil {
		r.config.DFBConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.DFBConfig.SetDefaults()
	r.config.DFBConfig.Headlines.SetDefaults()

	if r.config.UEFAConfig == nil {
		r.config.UEFAConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.UEFAConfig.Headlines == nil {
		r.config.UEFAConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.UEFAConfig.SetDefaults()
	r.config.UEFAConfig.Headlines.SetDefaults()

	if r.config.FIFAConfig == nil {
		r.config.FIFAConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.FIFAConfig.Headlines == nil {
		r.config.FIFAConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.FIFAConfig.SetDefaults()
	r.config.FIFAConfig.Headlines.SetDefaults()

	if r.config.SysConfig == nil {
		r.config.SysConfig = &sysboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.SysConfig.SetDefaults()

	if r.config.F1Config == nil {
		r.config.F1Config = &racingboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.F1Config.SetDefaults()

	if r.config.IRLConfig == nil {
		r.config.IRLConfig = &racingboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.IRLConfig.SetDefaults()

	if r.config.WeatherConfig == nil {
		r.config.WeatherConfig = &weatherboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.WeatherConfig.SetDefaults()

	if r.config.TVConfig == nil {
		r.config.TVConfig = &tvboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.TVConfig.SetDefaults()

	if r.config.NCAAWConfig == nil {
		r.config.NCAAWConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.NCAAWConfig.Headlines == nil {
		r.config.NCAAWConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.NCAAWConfig.SetDefaults()
	r.config.NCAAWConfig.Headlines.SetDefaults()

	if r.config.WNBAConfig == nil {
		r.config.WNBAConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.WNBAConfig.Headlines == nil {
		r.config.WNBAConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.WNBAConfig.SetDefaults()
	r.config.WNBAConfig.Headlines.SetDefaults()

	if r.config.LigueConfig == nil {
		r.config.LigueConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}

	if r.config.LigueConfig.Headlines == nil {
		r.config.LigueConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.LigueConfig.SetDefaults()
	r.config.LigueConfig.Headlines.SetDefaults()

	if r.config.SerieaConfig == nil {
		r.config.SerieaConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.SerieaConfig.Headlines == nil {
		r.config.SerieaConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.SerieaConfig.SetDefaults()
	r.config.SerieaConfig.Headlines.SetDefaults()

	if r.config.LaligaConfig == nil {
		r.config.LaligaConfig = &sportboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	if r.config.LaligaConfig.Headlines == nil {
		r.config.LaligaConfig.Headlines = &textboard.Config{
			StartEnabled: atomic.NewBool(false),
		}
	}
	r.config.LaligaConfig.SetDefaults()
	r.config.LaligaConfig.Headlines.SetDefaults()
}

func (r *rootArgs) getRGBMatrix(logger *zap.Logger) (matrix.Matrix, error) {
	var matrix matrix.Matrix
	logger.Info("initializing matrix",
		zap.Int("Cols", r.config.SportsMatrixConfig.HardwareConfig.Cols),
		zap.Int("Rows", r.config.SportsMatrixConfig.HardwareConfig.Rows),
		zap.Int("Brightness", r.config.SportsMatrixConfig.HardwareConfig.Brightness),
		zap.String("Mapping", r.config.SportsMatrixConfig.HardwareConfig.HardwareMapping),
	)

	// If we have configured the http server to listen on a privileged port (like 80),
	// we need to maintain root permissions
	if r.config.SportsMatrixConfig.HTTPListenPort < 1024 {
		r.config.SportsMatrixConfig.RuntimeOptions.DropPrivileges = -1
	}

	var err error
	matrix, err = rgb.NewRGBLedMatrix(r.config.SportsMatrixConfig.HardwareConfig, r.config.SportsMatrixConfig.RuntimeOptions, logger)

	return matrix, err
}

func (r *rootArgs) getTestMatrix(logger *zap.Logger) matrix.Matrix {
	logger.Info("initializing console matrix",
		zap.Int("Cols", r.config.SportsMatrixConfig.HardwareConfig.Cols),
		zap.Int("Rows", r.config.SportsMatrixConfig.HardwareConfig.Rows),
	)
	return matrix.NewConsoleMatrix(r.config.SportsMatrixConfig.HardwareConfig.Cols, r.config.SportsMatrixConfig.HardwareConfig.Rows, os.Stdout, logger)
}

func (r *rootArgs) getBoards(ctx context.Context, logger *zap.Logger) ([]board.Board, error) {
	bounds := image.Rect(0, 0, r.config.SportsMatrixConfig.HardwareConfig.Cols, r.config.SportsMatrixConfig.HardwareConfig.Rows)

	var boards []board.Board

	if r.config.NHLConfig != nil {
		start := len(boards)
		api, err := espnboard.NewNHL(ctx, logger)
		if err != nil {
			return boards, err
		}
		l, err := espnboard.GetLeaguer("nhl")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.NHLConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return boards, err
		}

		boards = append(boards, b)
		if r.config.NHLConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.NHLConfig.Headlines, logger, textboard.WithHalfSizeLogo())
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.NHLConfig, boards[start:])
	}

	if r.config.MLBConfig != nil {
		start := len(boards)
		api, err := espnboard.NewMLB(ctx, logger)
		if err != nil {
			return boards, err
		}

		m := &mlblive.MlbLive{
			Logger: logger,
		}

		if r.config.MLBConfig.LiveViewFont != nil {
			m.FontSize = r.config.MLBConfig.LiveViewFont.Size
		}

		opts := []sportboard.OptionFunc{
			sportboard.WithDetailedLiveRenderer(
				func(ctx context.Context, canvas board.Canvas, game sportboard.Game, hLogo *logo.Logo, aLogo *logo.Logo) error {
					mlbGame, ok := game.(*espnboard.Game)
					if !ok {
						return errors.New("unsupported sport for detailed renderer")
					}

					return m.RenderLive(ctx, canvas, mlbGame, hLogo, aLogo)
				},
			),
		}
		l, err := espnboard.GetLeaguer("mlb")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		opts = append(opts, sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo))

		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.MLBConfig, opts...)
		if err != nil {
			return boards, err
		}

		boards = append(boards, b)
		if r.config.MLBConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.MLBConfig.Headlines, logger, textboard.WithHalfSizeLogo())
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.MLBConfig, boards[start:])
	}
	if r.config.NCAAMConfig != nil {
		start := len(boards)
		api, err := espnboard.NewNCAAMensBasketball(ctx, logger)
		if err != nil {
			return boards, err
		}
		l, err := espnboard.GetLeaguer("ncaam")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)

		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.NCAAMConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return boards, err
		}

		boards = append(boards, b)
		if r.config.NCAAMConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.NCAAMConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.NCAAMConfig, boards[start:])
	}
	if r.config.NCAAFConfig != nil {
		start := len(boards)
		api, err := espnboard.NewNCAAF(ctx, logger)
		if err != nil {
			return boards, err
		}

		l, err := espnboard.GetLeaguer("ncaaf")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)

		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.NCAAFConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return boards, err
		}

		boards = append(boards, b)
		if r.config.NCAAFConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.NCAAFConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.NCAAFConfig, boards[start:])
	}
	if r.config.NBAConfig != nil {
		start := len(boards)
		api, err := espnboard.NewNBA(ctx, logger)
		if err != nil {
			return nil, err
		}
		l, err := espnboard.GetLeaguer("nba")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)

		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.NBAConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.NBAConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.NBAConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.NBAConfig, boards[start:])
	}
	if r.config.NFLConfig != nil {
		start := len(boards)
		api, err := espnboard.NewNFL(ctx, logger)
		if err != nil {
			return nil, err
		}
		l, err := espnboard.GetLeaguer("nfl")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)

		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.NFLConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.NFLConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.NFLConfig.Headlines, logger, textboard.WithHalfSizeLogo())
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.NFLConfig, boards[start:])
	}
	if r.config.MLSConfig != nil {
		start := len(boards)
		api, err := espnboard.NewMLS(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("mls")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.MLSConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.MLSConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.MLSConfig.Headlines, logger, textboard.WithHalfSizeLogo())
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.MLSConfig, boards[start:])
	}
	if r.config.NWSLConfig != nil {
		start := len(boards)
		api, err := espnboard.NewNWSL(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("nwsl")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.NWSLConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.NWSLConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.NWSLConfig.Headlines, logger, textboard.WithHalfSizeLogo())
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.NWSLConfig, boards[start:])
	}
	if r.config.EPLConfig != nil {
		start := len(boards)
		api, err := espnboard.NewEPL(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("epl")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.EPLConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.EPLConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.EPLConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.EPLConfig, boards[start:])
	}

	if r.config.DFLConfig != nil {
		start := len(boards)
		api, err := espnboard.NewDFL(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("dfl")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.DFLConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.DFLConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.DFLConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.DFLConfig, boards[start:])
	}

	if r.config.DFBConfig != nil {
		start := len(boards)
		api, err := espnboard.NewDFB(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("dfb")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.DFBConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.DFBConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.DFBConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.DFBConfig, boards[start:])
	}

	if r.config.UEFAConfig != nil {
		start := len(boards)
		api, err := espnboard.NewUEFA(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("uefa")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.UEFAConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.UEFAConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.UEFAConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.UEFAConfig, boards[start:])
	}

	if r.config.FIFAConfig != nil {
		start := len(boards)
		api, err := espnboard.NewFIFA(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("fifa")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.FIFAConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.FIFAConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.FIFAConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.FIFAConfig, boards[start:])
	}

	if r.config.ImageConfig != nil {
		start := len(boards)
		b, err := imageboard.New(r.config.ImageConfig, logger)
		if err != nil {
			return boards, err
		}
		boards = append(boards, b)

		r.addSection(&r.config.ImageConfig, boards[start:])
	}

	if r.config.ClockConfig != nil {
		start := len(boards)
		b, err := clock.New(r.config.ClockConfig, logger)
		if err != nil {
			return boards, err
		}
		boards = append(boards, b)

		r.addSection(&r.config.ClockConfig, boards[start:])
	}

	if r.config.SysConfig != nil {
		start := len(boards)
		b, err := sysboard.New(logger, r.config.SysConfig)
		if err != nil {
			return boards, err
		}
		boards = append(boards, b)

		r.addSection(&r.config.SysConfig, boards[start:])
	}

	if r.config.F1Config != nil {
		start := len(boards)
		api, err := espnracing.New(&espnracing.F1{}, logger)
		if err != nil {
			return nil, err
		}
		b, err := racingboard.New(api, logger, r.config.F1Config)
		if err != nil {
			return nil, err
		}
		boards = append(boards, b)

		r.addSection(&r.config.F1Config, boards[start:])
	}

	if r.config.IRLConfig != nil {
		start := len(boards)
		api, err := espnracing.New(&espnracing.IRL{}, logger)
		if err != nil {
			return nil, err
		}
		b, err := racingboard.New(api, logger, r.config.IRLConfig)
		if err != nil {
			return nil, err
		}
		boards = append(boards, b)

		r.addSection(&r.config.IRLConfig, boards[start:])
	}

	if r.config.WeatherConfig != nil {
		start := len(boards)
		current, forecast, err := weatherboard.New(r.config.WeatherConfig, logger)
		if err != nil {
			return nil, err
		}
		boards = append(boards, current, forecast)

		r.addSection(&r.config.WeatherConfig, boards[start:])
	}

	if r.config.TVConfig != nil {
		start := len(boards)
		b, err := tvboard.New(r.config.TVConfig, logger)
		if err != nil {
			return nil, err
		}
		boards = append(boards, b)

		r.addSection(&r.config.TVConfig, boards[start:])
	}

	if r.config.NCAAWConfig != nil {
		start := len(boards)
		api, err := espnboard.NewNCAAWomensBasketball(ctx, logger)
		if err != nil {
			return boards, err
		}
		l, err := espnboard.GetLeaguer("ncaaw")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)

		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.NCAAWConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return boards, err
		}

		boards = append(boards, b)
		if r.config.NCAAWConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.NCAAWConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.NCAAWConfig, boards[start:])
	}

	if r.config.WNBAConfig != nil {
		start := len(boards)
		api, err := espnboard.NewWNBA(ctx, logger)
		if err != nil {
			return nil, err
		}
		l, err := espnboard.GetLeaguer("wnba")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)

		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.WNBAConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.WNBAConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.WNBAConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.WNBAConfig, boards[start:])
	}

	if r.config.LigueConfig != nil {
		start := len(boards)
		api, err := espnboard.NewLigue(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("ligue")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.LigueConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.LigueConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.LigueConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.LigueConfig, boards[start:])
	}

	if r.config.SerieaConfig != nil {
		start := len(boards)
		api, err := espnboard.NewSerieA(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("seriea")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.SerieaConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.SerieaConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.SerieaConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.SerieaConfig, boards[start:])
	}

	if r.config.LaligaConfig != nil {
		start := len(boards)
		api, err := espnboard.NewLaLiga(ctx, logger)
		if err != nil {
			return nil, err
		}

		l, err := espnboard.GetLeaguer("laliga")
		if err != nil {
			return nil, err
		}
		headlineAPI := espnboard.NewHeadlines(l, logger)
		b, err := sportboard.New(ctx, api, bounds, r.todayT, logger, r.config.LaligaConfig,
			sportboard.WithLeagueLogoGetter(headlineAPI.GetLogo),
		)
		if err != nil {
			return nil, err
		}

		boards = append(boards, b)
		if r.config.LaligaConfig.Headlines != nil {
			b, err := textboard.New(headlineAPI, r.config.LaligaConfig.Headlines, logger)
			if err != nil {
				return nil, err
			}
			boards = append(boards, b)
		}

		r.addSection(&r.config.LaligaConfig, boards[start:])
	}

	return boards, nil
}
