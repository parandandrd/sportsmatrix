import React, { useCallback, useEffect, useState } from 'react';
import { BACKEND, ListBoards, MatrixPostRet, SetBoardEnabled, JumpToBoard } from './util';
import BoardPanel from './BoardPanel.js';
import { LogoSrc } from './Logo';
import './Dashboard.css';

// LivePreview shows what is on the panel right now. The endpoint enables the
// canvas on demand and the matrix drops the cached frame 20s after the last
// request, so this costs nothing while nobody is looking at it.
function LivePreview() {
    const [t, setT] = useState(() => Date.now());
    const [broken, setBroken] = useState(false);

    useEffect(() => {
        const id = setInterval(() => setT(Date.now()), 2000);
        return () => {
            clearInterval(id);
            fetch(`${BACKEND}/api/imgcanvas/disable`, { method: 'GET', mode: 'cors' })
                .catch(() => { /* going away anyway */ });
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

function BoardRow({ board, onChanged }) {
    const [open, setOpen] = useState(false);
    const [busy, setBusy] = useState(false);

    // there is no logo for every league -- NWSL has none -- so the row falls
    // back to just the name rather than a broken image
    const logo = board.kind === 'sport' || board.kind === 'racing'
        ? LogoSrc(board.path)
        : LogoSrc(board.path.split('/').pop());

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

    const jump = async () => {
        await JumpToBoard(board.name);
        await onChanged();
    };

    return (
        <>
            <div className="board-row">
                {logo ? <img className="board-logo" src={logo} alt="" /> : null}
                <button
                    className="board-name"
                    onClick={() => setOpen(!open)}
                    disabled={!board.kind}
                    title={board.kind ? 'Settings' : 'No settings for this board'}
                >
                    {board.name}
                </button>
                {board.inBetween ? <span className="board-tag">between</span> : null}
                <button className="board-jump" onClick={jump}>Jump</button>
                <label className="switch" title={`Enable ${board.name}`}>
                    <input
                        type="checkbox"
                        checked={board.enabled}
                        disabled={busy}
                        onChange={toggle}
                        aria-label={`Enable ${board.name}`}
                    />
                    <span />
                </label>
            </div>
            {open
                ? <div className="board-settings"><BoardPanel board={board} onChange={onChanged} /></div>
                : null}
        </>
    );
}

export default function Dashboard() {
    const [boards, setBoards] = useState([]);
    const [error, setError] = useState('');
    const [loaded, setLoaded] = useState(false);
    const [screenOn, setScreenOn] = useState(true);
    const [webBoardOn, setWebBoardOn] = useState(false);
    const [restarting, setRestarting] = useState(false);

    const refresh = useCallback(async () => {
        try {
            setBoards(await ListBoards());
            setError('');
        } catch (err) {
            setError(String(err));
        } finally {
            setLoaded(true);
        }

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

    useEffect(() => { refresh(); }, [refresh]);

    const call = async (method, body) => {
        await MatrixPostRet(`matrix.v1.Sportsmatrix/${method}`, body || '{}');
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

    return (
        <div className="dash">
            <LivePreview />

            <div className="section">
                <h2>Matrix</h2>
                <div className="controls">
                    <button className="btn-sm-matrix" onClick={() => setStatus(!screenOn, webBoardOn)}>
                        {screenOn ? 'Turn screen off' : 'Turn screen on'}
                    </button>
                    <button className="btn-sm-matrix" onClick={() => setStatus(screenOn, !webBoardOn)}>
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
            </div>

            <div className="section">
                <h2>Boards{boards.length > 0 ? ` (${boards.length})` : ''}</h2>
                <div className="boards">
                    {error
                        ? <p className="dash-msg error">Could not reach the matrix: {error}</p>
                        : null}
                    {!error && loaded && boards.length === 0
                        ? <p className="dash-msg">No boards are configured.</p>
                        : null}
                    {!error && !loaded
                        ? <p className="dash-msg">Loading...</p>
                        : null}
                    {boards.map((b) =>
                        <BoardRow key={b.name} board={b} onChanged={refresh} />)}
                </div>
            </div>
        </div>
    );
}
