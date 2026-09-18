import { useEffect, useState } from 'react';
import { BACKEND } from './util';

// The web board can show the panel's own frames, which are exactly what the
// LEDs show and cost the Pi nothing extra, or the boards drawn again at full
// size, which look smooth but have the Pi draw everything twice. The full-size
// drawing only runs while someone is following it.
export const PANEL = '/api/panel/frame';
export const FULL = '/api/imgcanvas/board';
export const VIEWS = ['panel', 'full'];

const VIEW_KEY = 'webBoardView';
// How long the Pi may hold a request waiting for the next frame, in seconds.
const WAIT = 10;
// Frames at most this often, in ms: a scroll changes the panel far faster than
// anyone needs to watch it in a browser.
const MIN_GAP = 50;
const RETRY = 2000;

// InitialView is ?view= from the URL, else what this browser last chose, else
// the panel.
export function InitialView(search, storage) {
    const asked = new URLSearchParams(search).get('view');
    if (VIEWS.includes(asked)) {
        return asked;
    }
    try {
        const saved = storage.getItem(VIEW_KEY);
        if (VIEWS.includes(saved)) {
            return saved;
        }
    } catch (e) {
        // storage can be missing or blocked; the default does fine
    }
    return 'panel';
}

// BrowserStorage is localStorage, or null where the browser blocks it.
export function BrowserStorage() {
    try {
        return window.localStorage;
    } catch (e) {
        return null;
    }
}

export function SaveView(storage, view) {
    try {
        storage.setItem(VIEW_KEY, view);
    } catch (e) {
        // not remembered, which is all this costs
    }
}

// FetchFrame asks for the frame at path, sending the tag of the one already
// shown so that an unchanged frame comes back as 304 rather than again. It
// resolves to { status: 'new', tag, blob }, { status: 'same' }, or
// { status: 'none' } when nothing has been drawn at that size yet.
export async function FetchFrame(path, tag, signal) {
    const headers = tag ? { 'If-None-Match': tag } : {};
    const resp = await fetch(`${BACKEND}${path}`, { cache: 'no-store', headers, signal });
    if (resp.status === 304) {
        return { status: 'same' };
    }
    if (resp.status === 204) {
        return { status: 'none' };
    }
    if (!resp.ok) {
        throw new Error(`${path}: ${resp.status}`);
    }
    return { status: 'new', tag: resp.headers.get('ETag'), blob: await resp.blob() };
}

function sleep(ms, signal) {
    return new Promise((resolve) => {
        const id = setTimeout(resolve, ms);
        signal.addEventListener('abort', () => {
            clearTimeout(id);
            resolve();
        }, { once: true });
    });
}

// FollowFrames calls show(blob, from) with each new frame of view until signal
// aborts. from is 'panel' or 'full'. The Pi holds each request until the frame
// changes, so a still board costs one request every WAIT seconds.
//
// A full-size frame only exists once a board has started while someone was
// watching, which can be minutes in. Until then it shows the panel.
export async function FollowFrames(view, show, signal) {
    const tags = {};
    const next = async (path, from, wait) => {
        const got = await FetchFrame(`${path}?wait=${wait}`, tags[from], signal);
        if (got.status === 'new') {
            tags[from] = got.tag;
            show(got.blob, from);
        }
        return got.status;
    };

    while (!signal.aborted) {
        try {
            if (view === 'full' && await next(FULL, 'full', WAIT) === 'none') {
                // Short waits here, to notice soon when the full size starts.
                delete tags.full;
                await next(PANEL, 'panel', 1);
            } else if (view !== 'full') {
                await next(PANEL, 'panel', WAIT);
            }
            await sleep(MIN_GAP, signal);
        } catch (e) {
            if (signal.aborted) {
                return;
            }
            // restarting, or a blip on the network
            await sleep(RETRY, signal);
        }
    }
}

// StopFull tells the Pi to stop drawing at full size straight away, rather
// than when it notices nobody has asked for a while.
export function StopFull() {
    fetch(`${BACKEND}/api/imgcanvas/disable`, { keepalive: true })
        .catch(() => { /* it stops by itself soon enough */ });
}

// usePageVisible is false while the page is in a background tab or on a
// locked phone, when nobody can see a frame.
export function usePageVisible() {
    const [visible, setVisible] = useState(() => !document.hidden);
    useEffect(() => {
        const update = () => setVisible(!document.hidden);
        document.addEventListener('visibilitychange', update);
        return () => document.removeEventListener('visibilitychange', update);
    }, []);
    return visible;
}

// useFrames follows view while on is true, and returns the newest frame as
// { src, from }. Leaving the full-size view, or the page, stops the Pi drawing
// it.
export function useFrames(view, on) {
    const [frame, setFrame] = useState({ src: null, from: null });

    useEffect(() => {
        if (!on) {
            return undefined;
        }
        const ctrl = new AbortController();
        let url = null;
        FollowFrames(view, (blob, from) => {
            const next = URL.createObjectURL(blob);
            setFrame({ src: next, from });
            if (url) {
                URL.revokeObjectURL(url);
            }
            url = next;
        }, ctrl.signal);

        return () => {
            ctrl.abort();
            if (view === 'full') {
                StopFull();
            }
        };
    }, [view, on]);

    return frame;
}
