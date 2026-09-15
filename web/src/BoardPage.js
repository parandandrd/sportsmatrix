import React, { useCallback, useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { ListBoards } from './util';
import BoardPanel from './BoardPanel.js';
import './Dashboard.css';

// BoardPage is one board's settings on its own URL, for bookmarking and for the
// nav menu. The board is looked up by the name ListBoards reports, so there is
// no route table to keep in step with the configured leagues.
export default function BoardPage() {
    const { name } = useParams();
    const [board, setBoard] = useState(null);
    const [state, setState] = useState('loading');

    const refresh = useCallback(async () => {
        try {
            const boards = await ListBoards();
            const found = boards.find((b) => b.name === name);
            setBoard(found || null);
            setState(found ? 'ok' : 'missing');
        } catch (err) {
            console.log('failed to list boards', err);
            setState('error');
        }
    }, [name]);

    useEffect(() => { refresh(); }, [refresh]);

    return (
        <div className="dash">
            <div className="section">
                <p className="back"><Link to="/">&larr; All boards</Link></p>
                {state === 'loading' ? <p className="dash-msg">Loading...</p> : null}
                {state === 'error' ? <p className="dash-msg error">Could not reach the matrix.</p> : null}
                {state === 'missing'
                    ? <p className="dash-msg">This instance has no board called "{name}".</p>
                    : null}
                {board
                    ? <div className="boards">
                        <div className="board-row"><span className="board-name">{board.name}</span></div>
                        <div className="board-settings">
                            <BoardPanel board={board} onChange={refresh} />
                        </div>
                    </div>
                    : null}
            </div>
        </div>
    );
}
