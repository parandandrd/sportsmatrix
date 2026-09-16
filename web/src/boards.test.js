import { DescribeBoard } from './util';
import { GroupBoards, MoveSection, SubLabel } from './boards';

// what the real Pi's ListBoards reported, trimmed, in the order the new server
// sends it: the config file's
const board = (name, rpcPath, section, extra = {}) =>
    DescribeBoard({ name, rpc_path: rpcPath, section, in_config_file: true, ...extra });

test('GroupBoards puts a league with its stats and headlines, in the order given', () => {
    const groups = GroupBoards([
        board('Clock', '/clock/board.v1.BasicBoard/', 'clockConfig', { enabled: true }),
        board('NHL', '/nhl/sport.v1.Sport/', 'nhlConfig', { enabled: true }),
        board('StatBoard: NHL', '/stat/nhl/board.v1.BasicBoard/', 'nhlConfig'),
        board('NHL Headlines', '/headlines/nhl/board.v1.BasicBoard/', 'nhlConfig'),
        board('StatBoard: PGA', '/stat/pga/board.v1.BasicBoard/', 'pga'),
        board('UEFA', '/uefa/sport.v1.Sport/', 'uefaConfig', { in_config_file: false }),
        board('UEFA Headlines', '/headlines/uefa/board.v1.BasicBoard/', 'uefaConfig', { in_config_file: false }),
    ]);

    expect(groups.map((g) => [g.main.name, g.subs.map(SubLabel), g.enabled, g.inConfigFile])).toEqual([
        ['Clock', [], true, true],
        ['NHL', ['Stats', 'Headlines'], true, true],
        // a section can be nothing but a stats board
        ['StatBoard: PGA', [], false, true],
        ['UEFA', ['Headlines'], false, false],
    ]);
});

test('a group is on when any of its boards is', () => {
    const [nfl] = GroupBoards([
        board('NFL', '/nfl/sport.v1.Sport/', 'nflConfig'),
        board('NFL Headlines', '/headlines/nfl/board.v1.BasicBoard/', 'nflConfig', { enabled: true }),
    ]);
    expect(nfl.enabled).toBe(true);
});

test('a server that reports no sections still groups by where boards mount', () => {
    // v0.0.3-beta.1, which is what the Pi runs today
    const groups = GroupBoards([
        DescribeBoard({ name: 'NHL', enabled: true, rpc_path: '/nhl/sport.v1.Sport/' }),
        DescribeBoard({ name: 'StatBoard: NHL', rpc_path: '/stat/nhl/board.v1.BasicBoard/' }),
        DescribeBoard({ name: 'NHL Headlines', rpc_path: '/headlines/nhl/board.v1.BasicBoard/' }),
        DescribeBoard({ name: 'Img', rpc_path: '/imageboard.v1.ImageBoard/' }),
        DescribeBoard({ name: 'Clock', enabled: true, rpc_path: '/clock/board.v1.BasicBoard/' }),
    ]);

    expect(groups.map((g) => [g.main.name, g.subs.length, g.inConfigFile])).toEqual([
        ['NHL', 2, true],
        ['Img', 0, true],
        ['Clock', 0, true],
    ]);
});

test('with no config file, nothing is singled out as missing from it', () => {
    const groups = GroupBoards([
        board('NHL', '/nhl/sport.v1.Sport/', 'nhlConfig', { in_config_file: false }),
        board('Clock', '/clock/board.v1.BasicBoard/', 'clockConfig', { in_config_file: false }),
    ]);
    expect(groups.every((g) => g.inConfigFile)).toBe(true);
});

test('GroupBoards copes with no list yet', () => {
    expect(GroupBoards(null)).toEqual([]);
});

test('MoveSection moves a group past the neighbour shown next to it', () => {
    const groups = GroupBoards([
        board('Clock', '/clock/board.v1.BasicBoard/', 'clockConfig', { enabled: true }),
        board('Sys', '/sys/board.v1.BasicBoard/', 'sysConfig'),
        board('NCAAF', '/ncaaf/sport.v1.Sport/', 'ncaafConfig', { enabled: true }),
        board('NHL', '/nhl/sport.v1.Sport/', 'nhlConfig', { enabled: true }),
        board('UEFA', '/uefa/sport.v1.Sport/', 'uefaConfig', { in_config_file: false }),
    ]);
    const on = groups.filter((g) => g.enabled);

    // NCAAF up past the clock; Sys, which isn't shown, stays put
    expect(MoveSection(groups, on, 'ncaafConfig', -1))
        .toEqual(['ncaafConfig', 'clockConfig', 'sysConfig', 'nhlConfig']);
    // the clock down past NCAAF
    expect(MoveSection(groups, on, 'clockConfig', 1))
        .toEqual(['sysConfig', 'ncaafConfig', 'clockConfig', 'nhlConfig']);
    // nowhere to go
    expect(MoveSection(groups, on, 'clockConfig', -1)).toBeNull();
    expect(MoveSection(groups, on, 'nhlConfig', 1)).toBeNull();

    // a section the file doesn't have is only named when it is the one moving
    const off = groups.filter((g) => !g.enabled);
    expect(MoveSection(groups, off, 'uefaConfig', -1))
        .toEqual(['clockConfig', 'uefaConfig', 'sysConfig', 'ncaafConfig', 'nhlConfig']);
});
