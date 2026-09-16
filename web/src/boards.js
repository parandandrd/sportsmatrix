import { useEffect, useSyncExternalStore } from 'react';
import { ListBoards } from './util';

// The board list is shared by every screen. Moving between them used to mean
// a blank "Loading..." until the Pi answered ListBoards again; now a screen
// draws from the last answer straight away and refreshes behind it.
let state = { boards: null, error: '' };
const listeners = new Set();
let latest = 0;
let inflight = null;

function publish(next) {
    state = { ...state, ...next };
    listeners.forEach((l) => l());
}

// RefreshBoards asks the Pi for the board list again. After a change, call it
// plainly: an answer to an older request never overwrites a newer one. A screen
// that is only mounting passes reuse, so the nav and the page it sits on share
// one request.
export function RefreshBoards({ reuse = false } = {}) {
    if (reuse && inflight) {
        return inflight;
    }

    const mine = ++latest;
    const request = ListBoards()
        .then((boards) => {
            if (mine === latest) {
                publish({ boards, error: '' });
            }
        })
        .catch((err) => {
            if (mine === latest) {
                publish({ error: String(err) });
            }
        })
        .finally(() => {
            if (inflight === request) {
                inflight = null;
            }
        });

    inflight = request;
    return request;
}

function subscribe(listener) {
    listeners.add(listener);
    return () => listeners.delete(listener);
}

// useBoards returns { boards, error }, where boards is null until the first
// answer, and refreshes the list when the calling screen mounts.
export function useBoards() {
    const current = useSyncExternalStore(subscribe, () => state);
    useEffect(() => { RefreshBoards({ reuse: true }); }, []);
    return current;
}

// sectionOf is the config file section a board belongs to. A server that does
// not report sections still says where each board mounts its service, and a
// league's stats and headlines boards mount under the league's own slug.
function sectionOf(board) {
    if (board.section) {
        return board.section;
    }
    const slug = board.path.replace(/^(stat|headlines)\//, '');
    return slug ? `slug:${slug}` : `name:${board.name}`;
}

// SubLabel names a board within its group: what it adds to the league.
export function SubLabel(board) {
    if (board.path.startsWith('stat/')) {
        return 'Stats';
    }
    if (board.path.startsWith('headlines/')) {
        return 'Headlines';
    }
    return board.name;
}

// GroupBoards gathers boards into the config file sections they are built
// from -- a league with its stats and headlines -- keeping the order ListBoards
// gives, which is the config file's. The first board of a section is its main
// one; ListBoards lists a league's own board ahead of the rest.
export function GroupBoards(boards) {
    const groups = [];
    const byKey = new Map();

    for (const board of boards || []) {
        const key = sectionOf(board);
        let group = byKey.get(key);
        if (!group) {
            group = { key, main: board, subs: [] };
            byKey.set(key, group);
            groups.push(group);
        } else {
            group.subs.push(board);
        }
    }

    for (const group of groups) {
        group.enabled = [group.main, ...group.subs].some((b) => b.enabled);
        group.inConfigFile = group.main.inConfigFile;
    }

    // With no config file at all nothing is "in" it, and saying so about every
    // board is no help to anyone.
    if (!groups.some((g) => g.inConfigFile)) {
        groups.forEach((g) => { g.inConfigFile = true; });
    }

    return groups;
}
