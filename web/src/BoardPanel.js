import React from 'react';
import Sport from './Sport.js';
import Racing from './Racing.js';
import ImageBoard from './ImageBoard.js';
import BasicBoard from './BasicBoard.js';

// BoardPanel renders the settings for one board. Which component to use comes
// from the board's own RPC path, reported by ListBoards, so nothing here has to
// know the list of leagues.
export default function BoardPanel({ board, onChange }) {
    const sync = onChange || (() => { });

    switch (board.kind) {
        case 'sport':
            return <Sport sport={board.path} id={board.path} doSync={sync} />;
        case 'racing':
            return <Racing sport={board.path} id={board.path} doSync={sync} />;
        case 'image':
            return <ImageBoard id={board.name} doSync={sync} />;
        case 'basic':
            return <BasicBoard id={board.name} name={board.name} path={board.path} doSync={sync} />;
        default:
            return (
                <p className="board-panel-empty">
                    This board has no settings of its own.
                </p>
            );
    }
}
