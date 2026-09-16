import { DelayChoices, DelaySeconds } from './delay';

test('DelaySeconds reads what the server writes', () => {
    expect(DelaySeconds('10s')).toBe(10);
    expect(DelaySeconds('1m30s')).toBe(90);
    expect(DelaySeconds('2m')).toBe(120);
    expect(DelaySeconds('1.5s')).toBe(1.5);
    expect(DelaySeconds('ten')).toBeNaN();
    expect(DelaySeconds('')).toBeNaN();
});

test('DelayChoices keep to what a board can take, and keep what it has', () => {
    expect(DelayChoices('10s', '10s')[0]).toBe('10s');
    expect(DelayChoices('10s', '5s')[0]).toBe('5s');
    expect(DelayChoices('25s', '10s')).toContain('25s');
    expect(DelayChoices('25s', '10s').indexOf('25s')).toBe(DelayChoices('25s', '10s').indexOf('20s') + 1);
});
