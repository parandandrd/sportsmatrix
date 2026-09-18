import React, { useCallback, useEffect, useState } from 'react';
import { CallRPC, MatrixPostRet, SetBoardEnabled, JumpToBoard } from './util';
import { useFrames, usePageVisible } from './frames';
import { GroupBoards, MoveSection, RefreshBoards, SubLabel, useBoards } from './boards';
import BoardPanel from './BoardPanel.js';
import MatrixSettings from './MatrixSettings.js';
import { LogoSrc } from './Logo';
import './Dashboard.css';

// LivePreview shows the panel's own frames: exactly what is on the LEDs, at no
// cost to the Pi. It only follows them while the page can be seen.
function LivePreview() {
    const visible = usePageVisible();
    const frame = useFrames('panel', visible);

    return (
        <div className="section">
            <h2>Live</h2>
            <div className="preview">
                {frame.src
                    ? <img src={frame.src} alt="What the panel is showing" />
                    : <p className="dash-msg">Waiting for the panel...</p>}
            </div>
            <p className="preview-note">What the panel is showing now.</p>
        </div>
    );
}

function logoFor(board) {
    if (board.kind === 'image') {
        return LogoSrc('img');
    }
    if (board.kind === 'sport' || board.kind === 'racing') {
        return LogoSrc(board.path);
    }
    return LogoSrc(board.path.split('/').pop());
}

function Switch({ board, label, onChanged, onError }) {
    const [busy, setBusy] = useState(false);

    const toggle = async () => {
        setBusy(true);
        try {
            await SetBoardEnabled(board.name, !board.enabled);
            onError('');
        } catch (err) {
            // the switch may still have flipped: a change that could not be
            // saved to the config file is made anyway
            onError(err.message);
        } finally {
            setBusy(false);
        }
        await onChanged();
    };

    return (
        <label className="switch" title={`Enable ${label || board.name}`}>
            <input
                type="checkbox"
                checked={board.enabled}
                disabled={busy}
                onChange={toggle}
                aria-label={`Enable ${board.name}`}
            />
            <span />
        </label>
    );
}

// BoardGroup is one config file section: a league's board, and the stats and
// headlines boards its section also builds.
function BoardGroup({ group, onChanged, tagUnconfigured, reorder }) {
    const [open, setOpen] = useState(false);
    const [problem, setProblem] = useState('');
    const { main, subs } = group;
    const logo = logoFor(main);

    const jump = async () => {
        await JumpToBoard(main.name);
        await onChanged();
    };

    return (
        <div className={`board-group${group.enabled ? '' : ' is-off'}`}>
            <div className="board-row">
                {logo
                    ? <img className="board-logo" src={logo} alt="" loading="lazy" decoding="async" />
                    : <span className="board-logo" />}
                <button
                    className="board-name"
                    onClick={() => setOpen(!open)}
                    disabled={!main.kind}
                    aria-expanded={open}
                    title={main.kind ? 'Settings' : 'No settings for this board'}
                >
                    {main.name}
                </button>
                {main.inBetween ? <span className="board-tag">between</span> : null}
                {tagUnconfigured && !group.inConfigFile
                    ? <span className="board-tag" title="Not in sportsmatrix.conf; running on defaults">not in conf</span>
                    : null}
                {reorder
                    ? <span className="board-move">
                        <button
                            onClick={() => reorder.move(group.key, -1)}
                            disabled={reorder.busy || reorder.first}
                            aria-label={`Move ${main.name} up`}
                        >&uarr;</button>
                        <button
                            onClick={() => reorder.move(group.key, 1)}
                            disabled={reorder.busy || reorder.last}
                            aria-label={`Move ${main.name} down`}
                        >&darr;</button>
                    </span>
                    : <button className="board-jump" onClick={jump}>Jump</button>}
                <Switch board={main} onChanged={onChanged} onError={setProblem} />
            </div>
            {subs.length > 0
                ? <div className="board-subs">
                    {subs.map((sub) =>
                        <div className="board-sub" key={sub.name}>
                            <span>{SubLabel(sub)}</span>
                            <Switch board={sub} label={SubLabel(sub)} onChanged={onChanged} onError={setProblem} />
                        </div>)}
                </div>
                : null}
            {problem ? <p className="dash-msg error board-problem" role="alert">{problem}</p> : null}
            {open
                ? <div className="board-settings">
                    <BoardPanel board={main} group={group} onChange={onChanged} onError={setProblem} />
                </div>
                : null}
        </div>
    );
}

// GroupList shows groups in the order given. With onMove, each can be moved up
// or down among them.
function GroupList({ groups, onChanged, tagUnconfigured, onMove, moving }) {
    return (
        <div className="boards">
            {groups.map((g, i) =>
                <BoardGroup
                    key={g.key}
                    group={g}
                    onChanged={onChanged}
                    tagUnconfigured={tagUnconfigured}
                    reorder={onMove
                        ? { move: (key, delta) => onMove(groups, key, delta), busy: moving !== '', first: i === 0, last: i === groups.length - 1 }
                        : null}
                />)}
        </div>
    );
}

export default function Dashboard() {
    const { boards, error } = useBoards();
    const [screenOn, setScreenOn] = useState(true);
    const [webBoardOn, setWebBoardOn] = useState(false);
    const [restarting, setRestarting] = useState(false);
    const [showOff, setShowOff] = useState(false);
    const [problem, setProblem] = useState('');
    const [pending, setPending] = useState('');
    const [reordering, setReordering] = useState(false);
    const [moving, setMoving] = useState('');
    const [orderProblem, setOrderProblem] = useState('');

    const refreshStatus = useCallback(async () => {
        try {
            const resp = await MatrixPostRet('matrix.v1.Sportsmatrix/GetStatus', '{}');
            if (resp.ok) {
                const dat = await resp.json();
                setScreenOn(Boolean(dat.screen_on));
                setWebBoardOn(Boolean(dat.webboard_on));
            }
        } catch (err) {
            console.log('failed to get matrix status', err);
        }
    }, []);

    const refresh = () => Promise.all([RefreshBoards(), refreshStatus()]);

    useEffect(() => { refreshStatus(); }, [refreshStatus]);

    // A failed call shows the server's reason, which is what the person
    // pressing the button needs -- "chromium is not installed", not "412".
    const call = async (method, body) => {
        setPending(method);
        try {
            await CallRPC(`matrix.v1.Sportsmatrix/${method}`, body || '{}');
            setProblem('');
        } catch (err) {
            setProblem(err.message);
        } finally {
            setPending('');
        }
        await refresh();
    };

    // SetStatus carries the whole status, so a field left out reads as false
    // and turns that thing off. Always send both.
    const setStatus = (screen, webBoard) =>
        call('SetStatus', JSON.stringify({ screen_on: screen, webboard_on: webBoard }));

    const restart = async () => {
        setRestarting(true);
        await MatrixPostRet('matrix.v1.Sportsmatrix/RestartService', '{}');
        setTimeout(() => { setRestarting(false); refresh(); }, 10000);
    };

    const groups = GroupBoards(boards);
    // the order lives in the config file, by section, which an older server
    // doesn't report
    const canReorder = groups.length > 1 && groups.every((g) => g.main.section);

    // Moving a board moves it on the panel too: the panel cycles through boards
    // in the config file's order.
    const move = async (visible, key, delta) => {
        const sections = MoveSection(groups, visible, key, delta);
        if (!sections) {
            return;
        }
        setMoving(key);
        try {
            await CallRPC('matrix.v1.Sportsmatrix/SetBoardOrder', { sections });
            setOrderProblem('');
        } catch (err) {
            setOrderProblem(err.message);
        }
        await RefreshBoards();
        setMoving('');
    };
    const onMove = reordering ? move : undefined;

    const on = groups.filter((g) => g.enabled);
    const off = groups.filter((g) => !g.enabled && g.inConfigFile);
    const unconfigured = groups.filter((g) => !g.enabled && !g.inConfigFile);
    const hidden = off.length + unconfigured.length;

    return (
        <div className="dash">
            <LivePreview />

            <div className="section">
                <h2>Matrix</h2>
                <div className="controls">
                    <button className="btn-sm-matrix" onClick={() => setStatus(!screenOn, webBoardOn)}>
                        {screenOn ? 'Turn screen off' : 'Turn screen on'}
                    </button>
                    <button
                        className="btn-sm-matrix"
                        onClick={() => setStatus(screenOn, !webBoardOn)}
                        disabled={pending === 'SetStatus'}
                        title="Shows the board full screen on a display plugged into the Pi"
                    >
                        {webBoardOn ? 'Stop web board' : 'Start web board'}
                    </button>
                    <button className="btn-sm-matrix" onClick={() => call('NextBoard')}>Next board</button>
                    <button className="btn-sm-matrix" onClick={() =>
                        call('SetAll', JSON.stringify({ enabled: true }))}>Enable all</button>
                    <button className="btn-sm-matrix" onClick={() =>
                        call('SetAll', JSON.stringify({ enabled: false }))}>Disable all</button>
                    <button className="btn-sm-matrix" onClick={() =>
                        call('SetLiveOnly', JSON.stringify({ live_only: true }))}>Live games only</button>
                    <button className="btn-sm-matrix" onClick={() =>
                        call('SetLiveOnly', JSON.stringify({ live_only: false }))}>All games</button>
                    <button className="btn-sm-matrix danger" onClick={restart} disabled={restarting}>
                        {restarting ? 'Restarting...' : 'Restart service'}
                    </button>
                </div>
                {problem ? <p className="dash-msg error" role="alert">{problem}</p> : null}
            </div>

            <MatrixSettings />

            {error ? <p className="dash-msg error">Could not reach the matrix: {error}</p> : null}
            {!error && boards === null ? <p className="dash-msg">Loading...</p> : null}
            {boards !== null && groups.length === 0
                ? <p className="dash-msg">No boards are configured.</p>
                : null}

            {on.length > 0
                ? <div className="section">
                    <div className="section-head">
                        <h2>On the panel ({on.length})</h2>
                        {canReorder
                            ? <button className="section-action" onClick={() => setReordering(!reordering)} aria-pressed={reordering}>
                                {reordering ? 'Done' : 'Reorder'}
                            </button>
                            : null}
                    </div>
                    {reordering
                        ? <p className="section-note">The panel shows boards in this order. It is saved to sportsmatrix.conf.</p>
                        : null}
                    {orderProblem ? <p className="dash-msg error" role="alert">{orderProblem}</p> : null}
                    <GroupList groups={on} onChanged={refresh} tagUnconfigured onMove={onMove} moving={moving} />
                </div>
                : null}

            {boards !== null && on.length === 0 && groups.length > 0
                ? <div className="section">
                    <h2>On the panel</h2>
                    <p className="dash-msg">Every board is off, so the panel is blank.</p>
                </div>
                : null}

            {hidden > 0
                ? <div className="section">
                    <button
                        className="section-toggle"
                        onClick={() => setShowOff(!showOff)}
                        aria-expanded={showOff}
                    >
                        <h2>Off ({hidden})</h2>
                        <span>{showOff ? 'Hide' : 'Show'}</span>
                    </button>
                    {showOff && off.length > 0
                        ? <GroupList groups={off} onChanged={refresh} onMove={onMove} moving={moving} />
                        : null}
                    {showOff && unconfigured.length > 0
                        ? <>
                            <p className="section-note">
                                Not in sportsmatrix.conf. These run on defaults.
                            </p>
                            <GroupList groups={unconfigured} onChanged={refresh} onMove={onMove} moving={moving} />
                        </>
                        : null}
                </div>
                : null}
        </div>
    );
}
