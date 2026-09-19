import { useEffect, useState } from 'react';
import { BACKEND } from './util';

// The web UI shows the panel's own frames: exactly what the LEDs show, kept by
// the matrix driver as it shows them, at no cost to the Pi.
export const PANEL = '/api/panel/frame';

// How long the Pi may hold a request waiting for the next frame, in seconds.
const WAIT = 10;
// Frames at most this often, in ms: a scroll changes the panel far faster than
// anyone needs to watch it in a browser.
const MIN_GAP = 50;
const RETRY = 2000;

// FetchFrame asks for the panel's frame, sending the tag of the one already
// shown so that an unchanged frame comes back as 304 rather than again, and
// letting the Pi hold the request up to wait seconds for it to change. It
// resolves to { status: 'new', tag, blob } or { status: 'same' }.
export async function FetchFrame(tag, wait, signal) {
    const headers = tag ? { 'If-None-Match': tag } : {};
    const resp = await fetch(`${BACKEND}${PANEL}?wait=${wait}`, { cache: 'no-store', headers, signal });
    if (resp.status === 304) {
        return { status: 'same' };
    }
    if (!resp.ok) {
        throw new Error(`${PANEL}: ${resp.status}`);
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

// FollowFrames calls show(blob) with each new frame of the panel until signal
// aborts. The Pi holds each request until the frame changes, so a still board
// costs one request every WAIT seconds.
export async function FollowFrames(show, signal) {
    let tag = null;
    while (!signal.aborted) {
        try {
            const got = await FetchFrame(tag, WAIT, signal);
            if (got.status === 'new') {
                tag = got.tag;
                show(got.blob);
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

// useFrame follows the panel while on is true, and returns the newest frame as
// an object URL, or null before the first.
export function useFrame(on) {
    const [src, setSrc] = useState(null);

    useEffect(() => {
        if (!on) {
            return undefined;
        }
        const ctrl = new AbortController();
        let url = null;
        FollowFrames((blob) => {
            const next = URL.createObjectURL(blob);
            setSrc(next);
            if (url) {
                URL.revokeObjectURL(url);
            }
            url = next;
        }, ctrl.signal);

        return () => ctrl.abort();
    }, [on]);

    return src;
}
