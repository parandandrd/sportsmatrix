import { useCallback, useEffect, useState } from 'react';

// Full screen goes through the standard API, or Safari's webkit-prefixed one on
// iPad. iPhone Safari has neither for anything but video, so the button is only
// offered where it can work.

export function FullscreenSupported(doc) {
    return Boolean(doc.fullscreenEnabled || doc.webkitFullscreenEnabled);
}

export function FullscreenElement(doc) {
    return doc.fullscreenElement || doc.webkitFullscreenElement || null;
}

// EnterFullscreen resolves once el is full screen, or rejects if the browser
// refuses. Older Safari's prefixed call returns nothing rather than a promise.
export function EnterFullscreen(el) {
    if (el.requestFullscreen) {
        return el.requestFullscreen();
    }
    if (el.webkitRequestFullscreen) {
        return Promise.resolve(el.webkitRequestFullscreen());
    }
    return Promise.reject(new Error('full screen is not available here'));
}

export function ExitFullscreen(doc) {
    if (doc.exitFullscreen) {
        return doc.exitFullscreen();
    }
    if (doc.webkitExitFullscreen) {
        return Promise.resolve(doc.webkitExitFullscreen());
    }
    return Promise.resolve();
}

// useFullscreen puts the element ref points at in and out of full screen, and
// reports whether it is. Esc, or the back gesture on a phone, leaves it too.
export function useFullscreen(ref) {
    const [active, setActive] = useState(false);
    const supported = FullscreenSupported(document);

    useEffect(() => {
        const update = () => setActive(ref.current !== null && FullscreenElement(document) === ref.current);
        document.addEventListener('fullscreenchange', update);
        document.addEventListener('webkitfullscreenchange', update);
        return () => {
            document.removeEventListener('fullscreenchange', update);
            document.removeEventListener('webkitfullscreenchange', update);
        };
    }, [ref]);

    const toggle = useCallback(() => {
        if (!ref.current) {
            return;
        }
        const leaving = FullscreenElement(document) === ref.current;
        (leaving ? ExitFullscreen(document) : EnterFullscreen(ref.current))
            .catch(() => { /* refused: it stays as it was */ });
    }, [ref]);

    return { supported, active, toggle };
}
