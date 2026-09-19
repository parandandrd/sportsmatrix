import React, { useEffect } from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import { useFrame, usePageVisible } from './frames';
import './Board.css';

// Board is the live board, meant to be left up on a screen: the panel's own
// frames, blown up with square pixels.
export default function Board() {
    const visible = usePageVisible();
    const src = useFrame(visible);

    useEffect(() => {
        document.body.style.backgroundColor = 'black';
        return () => {
            document.body.style.backgroundColor = '';
        };
    }, []);

    return (
        <div className="webboard">
            {src ? <img className="webboard-frame" src={src} alt="What the panel is showing" /> : null}
        </div>
    );
}
