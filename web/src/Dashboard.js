import React, { useCallback, useEffect, useState } from 'react';
import { BACKEND, MatrixPostRet, SetBoardEnabled, JumpToBoard } from './util';
import { GroupBoards, RefreshBoards, SubLabel, useBoards } from './boards';
import BoardPanel from './BoardPanel.js';
import { LogoSrc } from './Logo';
import './Dashboard.css';

// LivePreview shows what is on the panel right now. Each request keeps the Pi
// drawing every board a second time, at the web board's size, so it only polls
// while the page can actually be seen: not from a background tab, or a phone
// locked with the dashboard still open.
function LivePreview() {
    const [t, setT] = useState(() => Date.now());
    const [broken, setBroken] = useState(false);

    useEffect(() => {
        let id = null;

        const disable = () => {
            fetch(`${BACKEND}/api/imgcanvas/disable`, { method: 'GET', mode: 'cors' })
                .catch(() => { /* going away anyway */ });
        };
        const start = () => {
            if (id === null) {
                id = setInterval(() => setT(Date.now()), 2000);
            }
        };
        const stop = () => {
            if (id !== null) {
                clearInterval(id);
                id = null;
            }
            disable();
        };
        const onVisibility = () => {
            if (document.hidden) {
                stop();
            } else {
                setT(Date.now());
                start();
            }
        };

        if (!document.hidden) {
            start();
        }
        document.addEventListener('visibilitychange', onVisibility);

        return () => {
            document.removeEventListener('visibilitychange', onVisibility);
            stop();
        };
    }, []);

    return (
        <div className="section">
            <h2>Live</h2>
            <div className="preview">
                {broken
                    ? <p className="dash-msg">No preview available.</p>
                    : <img
                        src={`${BACKEND}/api/imgcanvas/board?${t}`}
                        alt="Current matrix output"
                        onError={() => setBroken(true)}
                    />}
            </div>
            <p className="preview-note">Updates every 2 seconds.</p>
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

function Switch({ board, label, onChanged }) {
    const [busy, setBusy] = useState(false);

    const toggle = async () => {
        setBusy(true);
        try {
            await SetBoardEnabled(board.name, !board.enabled);
            await onChanged();
        } catch (err) {
            console.log('failed to toggle board', board.name, err);
        } finally {
            setBusy(false);
        }
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
function BoardGroup({ group, onChanged, tagUnconfigured }) {
    const [open, setOpen] = useState(false);
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
                <button className="board-jump" onClick={jump}>Jump</button>
                <Switch board={main} onChanged={onChanged} />
            </div>
            {subs.length > 0
                ? <div className="board-subs">
                    {subs.map((sub) =>
                        <div className="board-sub" key={sub.name}>
                            <span>{SubLabel(sub)}</span>
                            <Switch board={sub} label={SubLabel(sub)} onChanged={onChanged} />
                        </div>)}
                </div>
                : null}
            {open
                ? <div className="board-settings">
                    <BoardPanel board={main} group={group} onChange={onChanged} />
                </div>
                : null}
        </div>
    );
}

function GroupList({ groups, onChanged, tagUnconfigured }) {
    return (
        <div className="boards">
            {groups.map((g) =>
                <BoardGroup key={g.key} group={g} onChanged={onChanged} tagUnconfigured={tagUnconfigured} />)}
        </div>
    );
}

// twirpMessage pulls the reason out of a failed Twirp call, which is what the
// person pressing the button needs -- "no browser installed", not "412".
async function twirpMessage(resp) {
    try {
        const body = await resp.json();
        if (body && body.msg) {
            return body.msg;
        }
    } catch (e) {
        // not a Twirp error body
    }
    return `${resp.status} ${resp.statusText}`;
}

export default function Dashboard() {
    const { boards, error } = useBoards();
    const [screenOn, setScreenOn] = useState(true);
    const [webBoardOn, setWebBoardOn] = useState(false);
    const [restarting, setRestarting] = useState(false);
    const [showOff, setShowOff] = useState(false);
    const [problem, setProblem] = useState('');
    const [pending, setPending] = useState('');

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

    const call = async (method, body) => {
        setPending(method);
        try {
            const resp = await MatrixPostRet(`matrix.v1.Sportsmatrix/${method}`, body || '{}');
            setProblem(resp.ok ? '' : await twirpMessage(resp));
        } catch (err) {
            setProblem(String(err));
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

            {error ? <p className="dash-msg error">Could not reach the matrix: {error}</p> : null}
            {!error && boards === null ? <p className="dash-msg">Loading...</p> : null}
            {boards !== null && groups.length === 0
                ? <p className="dash-msg">No boards are configured.</p>
                : null}

            {on.length > 0
                ? <div className="section">
                    <h2>On the panel ({on.length})</h2>
                    <GroupList groups={on} onChanged={refresh} tagUnconfigured />
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
                    {showOff && off.length > 0 ? <GroupList groups={off} onChanged={refresh} /> : null}
                    {showOff && unconfigured.length > 0
                        ? <>
                            <p className="section-note">
                                Not in sportsmatrix.conf. These run on defaults.
                            </p>
                            <GroupList groups={unconfigured} onChanged={refresh} />
                        </>
                        : null}
                </div>
                : null}
        </div>
    );
}
