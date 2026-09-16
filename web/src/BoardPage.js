import React, { useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { GroupBoards, RefreshBoards, useBoards } from './boards';
import BoardPanel from './BoardPanel.js';
import './Dashboard.css';

// BoardPage is one board's settings on its own URL, for bookmarking and for the
// nav menu. The board is looked up by the name ListBoards reports, so there is
// no route table to keep in step with the configured leagues. Arriving from
// another screen, the list is already here, so the settings start loading at
// once instead of after another round trip for the list.
export default function BoardPage() {
    const { name } = useParams();
    const { boards, error } = useBoards();
    const [problem, setProblem] = useState('');

    const group = GroupBoards(boards).find((g) => [g.main, ...g.subs].some((b) => b.name === name));
    const board = group ? [group.main, ...group.subs].find((b) => b.name === name) : null;

    return (
        <div className="dash">
            <div className="section">
                <p className="back"><Link to="/">&larr; All boards</Link></p>
                {boards === null && !error ? <p className="dash-msg">Loading...</p> : null}
                {boards === null && error ? <p className="dash-msg error">Could not reach the matrix.</p> : null}
                {boards !== null && !board
                    ? <p className="dash-msg">This instance has no board called "{name}".</p>
                    : null}
                {board
                    ? <div className="boards">
                        <div className="board-row"><span className="board-name">{board.name}</span></div>
                        <div className="board-settings">
                            <BoardPanel
                                key={board.name}
                                board={board}
                                group={board === group.main ? group : undefined}
                                onChange={() => RefreshBoards()}
                                onError={setProblem}
                            />
                        </div>
                        {problem ? <p className="dash-msg error board-problem" role="alert">{problem}</p> : null}
                    </div>
                    : null}
            </div>
        </div>
    );
}
