import { FetchFrame, FollowFrames } from './frames';

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
    await expect(FetchFrame('"b-1"', 10)).resolves.toEqual(
        { status: 'new', tag: '"b-2"', blob: 'blob "b-2"' });
    expect(global.fetch.mock.calls[0][0]).toMatch(/\/api\/panel\/frame\?wait=10$/);
    expect(global.fetch.mock.calls[0][1].headers).toEqual({ 'If-None-Match': '"b-1"' });

    global.fetch = jest.fn(async () => reply(304));
    await expect(FetchFrame('"b-2"', 10)).resolves.toEqual({ status: 'same' });

    global.fetch = jest.fn(async () => reply(200, '"b-1"'));
    await FetchFrame(null, 10);
    expect(global.fetch.mock.calls[0][1].headers).toEqual({});

    global.fetch = jest.fn(async () => reply(500));
    await expect(FetchFrame(null, 10)).rejects.toThrow('500');
});

test('FollowFrames shows each new frame, and asks with the last one it has', async () => {
    const ctrl = new AbortController();
    const answers = [reply(200, '"p-1"'), reply(304), reply(200, '"p-2"')];
    const asked = [];
    global.fetch = jest.fn(async (url, opts) => {
        asked.push(opts.headers['If-None-Match'] || '-');
        const next = answers.shift();
        if (!next) {
            ctrl.abort();
            throw new Error('aborted');
        }
        return next;
    });

    const shown = [];
    await FollowFrames((blob) => shown.push(blob), ctrl.signal);

    expect(shown).toEqual(['blob "p-1"', 'blob "p-2"']);
    expect(asked).toEqual(['-', '"p-1"', '"p-1"', '"p-2"']);
});
