import React, { useEffect, useRef } from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import { useFrame, usePageVisible } from './frames';
import { useFullscreen } from './fullscreen';
import './Board.css';

// Board is the live board, meant to be left up on a screen: the panel's own
// frames, blown up with square pixels. Full screen from the button or a double
// click; Esc leaves it.
export default function Board() {
    const visible = usePageVisible();
    const src = useFrame(visible);
    const page = useRef(null);
    const full = useFullscreen(page);

    useEffect(() => {
        document.body.style.backgroundColor = 'black';
        return () => {
            document.body.style.backgroundColor = '';
        };
    }, []);

    return (
        <div className="webboard" ref={page} onDoubleClick={full.supported ? full.toggle : undefined}>
            {src ? <img className="webboard-frame" src={src} alt="What the panel is showing" /> : null}
            {full.supported && !full.active
                ? <button className="webboard-fullscreen" onClick={full.toggle}>Full screen</button>
                : null}
        </div>
    );
}
