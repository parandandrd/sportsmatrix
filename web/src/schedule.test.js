import { DailyCron, ParseCron, ToCron } from './schedule';

test('daily times are edited as times', () => {
    // what the Pi's config has
    expect(ParseCron('0 0 * * *')).toEqual({ time: '00:00' });
    expect(ParseCron('23 16 * * *')).toEqual({ time: '16:23' });
    expect(DailyCron('07:30')).toBe('30 7 * * *');
    expect(ToCron(ParseCron('23 16 * * *'))).toBe('23 16 * * *');
});

test('anything else is kept as written', () => {
    for (const spec of ['0 23 * * 0-4', '@midnight', '*/15 * * * *', '0 7 1 * *', '75 7 * * *']) {
        expect(ParseCron(spec)).toEqual({ spec });
        expect(ToCron(ParseCron(spec))).toBe(spec);
    }
});
