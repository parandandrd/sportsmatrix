package config

import (
	calendarboard "github.com/parandandrd/sportsmatrix/internal/board/calendar"
	clock "github.com/parandandrd/sportsmatrix/internal/board/clock"
	imageboard "github.com/parandandrd/sportsmatrix/internal/board/image"
	racingboard "github.com/parandandrd/sportsmatrix/internal/board/racing"
	sportboard "github.com/parandandrd/sportsmatrix/internal/board/sport"
	statboard "github.com/parandandrd/sportsmatrix/internal/board/stat"
	sysboard "github.com/parandandrd/sportsmatrix/internal/board/sys"
	tvboard "github.com/parandandrd/sportsmatrix/internal/board/tv"
	weatherboard "github.com/parandandrd/sportsmatrix/internal/board/weather"
	"github.com/parandandrd/sportsmatrix/internal/sportsmatrix"
)

// Config holds configuration for the RGB matrix and all of its supported Boards
type Config struct {
	Debug              bool                  `json:"debug"`
	EnableNHL          bool                  `json:"enableNHL,omitempty"`
	NHLConfig          *sportboard.Config    `json:"nhlConfig,omitempty"`
	MLBConfig          *sportboard.Config    `json:"mlbConfig,omitempty"`
	NCAAMConfig        *sportboard.Config    `json:"ncaamConfig,omitempty"`
	NCAAFConfig        *sportboard.Config    `json:"ncaafConfig,omitempty"`
	NBAConfig          *sportboard.Config    `json:"nbaConfig,omitempty"`
	NFLConfig          *sportboard.Config    `json:"nflConfig,omitempty"`
	MLSConfig          *sportboard.Config    `json:"mlsConfig,omitempty"`
	NWSLConfig         *sportboard.Config    `json:"nwslConfig,omitempty"`
	EPLConfig          *sportboard.Config    `json:"eplConfig,omitempty"`
	DFLConfig          *sportboard.Config    `json:"dflConfig,omitempty"`
	DFBConfig          *sportboard.Config    `json:"dfbConfig,omitempty"`
	UEFAConfig         *sportboard.Config    `json:"uefaConfig,omitempty"`
	FIFAConfig         *sportboard.Config    `json:"fifaConfig,omitempty"`
	ImageConfig        *imageboard.Config    `json:"imageConfig"`
	ClockConfig        *clock.Config         `json:"clockConfig"`
	SysConfig          *sysboard.Config      `json:"sysConfig"`
	PGA                *statboard.Config     `json:"pga"`
	SportsMatrixConfig *sportsmatrix.Config  `json:"sportsMatrixConfig,omitempty"`
	F1Config           *racingboard.Config   `json:"f1Config"`
	IRLConfig          *racingboard.Config   `json:"irlConfig"`
	CalenderConfig     *calendarboard.Config `json:"calendarConfig"`
	WeatherConfig      *weatherboard.Config  `json:"weatherConfig"`
	TVConfig           *tvboard.Config       `json:"tvConfig"`
	NCAAWConfig        *sportboard.Config    `json:"ncaawConfig,omitempty"`
	WNBAConfig         *sportboard.Config    `json:"wnbaConfig,omitempty"`
	LigueConfig        *sportboard.Config    `json:"ligueConfig,omitempty"`
	SerieaConfig       *sportboard.Config    `json:"serieaConfig,omitempty"`
	LaligaConfig       *sportboard.Config    `json:"laligaConfig,omitempty"`
	XFLConfig          *sportboard.Config    `json:"xflConfig,omitempty"`
}
