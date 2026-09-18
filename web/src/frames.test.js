import { FetchFrame, FollowFrames, InitialView, SaveView } from './frames';

function storage(saved) {
    const items = saved ? { webBoardView: saved } : {};
    return {
        getItem: (k) => (k in items ? items[k] : null),
        setItem: (k, v) => { items[k] = v; },
    };
}

test('InitialView: the URL, then what was chosen last, then the panel', () => {
    expect(InitialView('?view=full', storage('panel'))).toBe('full');
    expect(InitialView('', storage('full'))).toBe('full');
    expect(InitialView('?view=sideways', storage('full'))).toBe('full');
    expect(InitialView('', storage('sideways'))).toBe('panel');
    expect(InitialView('', storage())).toBe('panel');
    expect(InitialView('', null)).toBe('panel');
});

test('SaveView remembers the choice, and shrugs off blocked storage', () => {
    const s = storage();
    SaveView(s, 'full');
    expect(InitialView('', s)).toBe('full');
    expect(() => SaveView(null, 'full')).not.toThrow();
});

function reply(status, tag) {
    return {
        status,
        ok: status >= 200 && status < 300,
        headers: { get: (h) => (h === 'ETag' ? tag : null) },
        blob: async () => `blob ${tag}`,
    };
}

afterEach(() => {
    delete global.fetch;
});

test('FetchFrame sends the tag it has, and reads the answer', async () => {
    global.fetch = jest.fn(async () => reply(200, '"b-2"'));
    await expect(FetchFrame('/api/panel/frame', '"b-1"')).resolves.toEqual(
        { status: 'new', tag: '"b-2"', blob: 'blob "b-2"' });
    expect(global.fetch.mock.calls[0][1].headers).toEqual({ 'If-None-Match': '"b-1"' });

    global.fetch = jest.fn(async () => reply(304));
    await expect(FetchFrame('/api/panel/frame', '"b-2"')).resolves.toEqual({ status: 'same' });

    global.fetch = jest.fn(async () => reply(204));
    await expect(FetchFrame('/api/imgcanvas/board')).resolves.toEqual({ status: 'none' });
    expect(global.fetch.mock.calls[0][1].headers).toEqual({});

    global.fetch = jest.fn(async () => reply(500));
    await expect(FetchFrame('/api/panel/frame')).rejects.toThrow('500');
});

test('FollowFrames shows the panel until the full size has a frame', async () => {
    const ctrl = new AbortController();
    const answers = {
        '/api/imgcanvas/board': [reply(204), reply(200, '"f-1"')],
        '/api/panel/frame': [reply(200, '"p-1"')],
    };
    const asked = [];
    global.fetch = jest.fn(async (url, opts) => {
        const path = new URL(url).pathname;
        asked.push(`${path}${new URL(url).search} ${opts.headers['If-None-Match'] || '-'}`);
        const next = answers[path].shift();
        if (!next) {
            ctrl.abort();
            throw new Error('aborted');
        }
        return next;
    });

    const shown = [];
    await FollowFrames('full', (blob, from) => shown.push(`${from}: ${blob}`), ctrl.signal);

    expect(shown).toEqual(['panel: blob "p-1"', 'full: blob "f-1"']);
    expect(asked).toEqual([
        '/api/imgcanvas/board?wait=10 -',
        '/api/panel/frame?wait=1 -',
        '/api/imgcanvas/board?wait=10 -',
        '/api/imgcanvas/board?wait=10 "f-1"',
    ]);
});
