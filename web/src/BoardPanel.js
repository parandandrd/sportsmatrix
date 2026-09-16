import React from 'react';
import Sport from './Sport.js';
import Racing from './Racing.js';
import ImageBoard from './ImageBoard.js';
import BasicBoard from './BasicBoard.js';

// BoardPanel renders the settings for one board. Which component to use comes
// from the board's own RPC path, reported by ListBoards, so nothing here has to
// know the list of leagues.
//
// group, when given, is the board's config section. It says whether a league
// has stats or headlines boards at all, so the panel doesn't ask services that
// aren't there.
export default function BoardPanel({ board, group, onChange, onError }) {
    const sync = onChange || (() => { });
    const has = (prefix) => (group ? group.subs.some((b) => b.path.startsWith(prefix)) : undefined);

    switch (board.kind) {
        case 'sport':
            return <Sport sport={board.path} name={board.name} id={board.path}
                stats={has('stat/')} headlines={has('headlines/')} doSync={sync} onError={onError} />;
        case 'racing':
            return <Racing sport={board.path} name={board.name} id={board.path} doSync={sync} onError={onError} />;
        case 'image':
            return <ImageBoard id={board.name} name={board.name} doSync={sync} onError={onError} />;
        case 'basic':
            return <BasicBoard id={board.name} name={board.name} path={board.path} doSync={sync} onError={onError} />;
        default:
            return (
                <p className="board-panel-empty">
                    This board has no settings of its own.
                </p>
            );
    }
}
