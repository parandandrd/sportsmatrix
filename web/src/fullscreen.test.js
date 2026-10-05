import { EnterFullscreen, ExitFullscreen, FullscreenElement, FullscreenSupported } from './fullscreen';

test('FullscreenSupported: the standard API, or Safari on iPad, but not iPhone', () => {
    expect(FullscreenSupported({ fullscreenEnabled: true })).toBe(true);
    expect(FullscreenSupported({ webkitFullscreenEnabled: true })).toBe(true);
    expect(FullscreenSupported({ fullscreenEnabled: false })).toBe(false);
    expect(FullscreenSupported({})).toBe(false);
});

test('FullscreenElement reads either API', () => {
    const el = {};
    expect(FullscreenElement({ fullscreenElement: el })).toBe(el);
    expect(FullscreenElement({ webkitFullscreenElement: el })).toBe(el);
    expect(FullscreenElement({})).toBeNull();
});

test('EnterFullscreen prefers the standard call, and always gives a promise', async () => {
    const standard = { requestFullscreen: vi.fn(async () => 'standard'), webkitRequestFullscreen: vi.fn() };
    await expect(EnterFullscreen(standard)).resolves.toBe('standard');
    expect(standard.webkitRequestFullscreen).not.toHaveBeenCalled();

    const safari = { webkitRequestFullscreen: vi.fn(() => undefined) };
    await expect(EnterFullscreen(safari)).resolves.toBeUndefined();
    expect(safari.webkitRequestFullscreen).toHaveBeenCalled();

    await expect(EnterFullscreen({})).rejects.toThrow('not available');
});

test('ExitFullscreen uses whichever the browser has', async () => {
    const standard = { exitFullscreen: vi.fn(async () => 'left') };
    await expect(ExitFullscreen(standard)).resolves.toBe('left');

    const safari = { webkitExitFullscreen: vi.fn() };
    await ExitFullscreen(safari);
    expect(safari.webkitExitFullscreen).toHaveBeenCalled();

    await expect(ExitFullscreen({})).resolves.toBeUndefined();
});
