import { describe, expect, it } from 'vitest';
import { MARK, quoteTag, readQuestion, toQuestion, type Quote } from './quotes';

const q = (text: string, turn = 7, start = 0, end = text.length): Quote => ({ text, turn, start, end });

describe('toQuestion', () => {
  it('marks where each quote sits in the words, the quotes attached in order', () => {
    expect(toQuestion(['比较', q('进程'), '和', q('线程', 7, 40, 42), '的区别'])).toEqual({
      text: `比较${MARK}和${MARK}的区别`,
      attachments: [
        { kind: 'selection', text: '进程', turn: 7, start: 0, end: 2 },
        { kind: 'selection', text: '线程', turn: 7, start: 40, end: 42 },
      ],
    });
  });

  it('trims the words around the question but not between them and a quote', () => {
    expect(toQuestion(['  ', q('进程'), ' 是什么？ \n'])).toMatchObject({ text: `${MARK} 是什么？` });
  });

  it('keeps no mark the reader typed or pasted themselves', () => {
    expect(toQuestion([`a${MARK}b`])).toEqual({ text: 'ab', attachments: [] });
  });
});

describe('readQuestion', () => {
  it('puts each quote back where its mark is', () => {
    const asked = toQuestion(['比较', q('进程'), '和', q('线程'), '的区别']);
    expect(readQuestion(asked)).toEqual({
      above: [],
      pieces: ['比较', asked.attachments[0], '和', asked.attachments[1], '的区别'],
    });
  });

  it('shows a selection with no mark (one carried on a phone) above the question', () => {
    const sel = { kind: 'selection' as const, text: '一段话' };
    expect(readQuestion({ text: '这是什么意思？', attachments: [sel] })).toEqual({ above: [sel], pieces: ['这是什么意思？'] });
  });
});

describe('quoteTag', () => {
  const turnNo = (id: number) => (id === 7 ? 3 : null);

  it('shows a word or a phrase as it is', () => {
    expect(quoteTag({ kind: 'selection', text: '上下文窗口', turn: 7, start: 3, end: 8 }, turnNo)).toEqual({ short: true, label: '上下文窗口' });
  });

  it('shortens a longer passage to its turn and where it sits in the reply', () => {
    const text = '这是一段很长的回复内容，超过了直接显示的长度';
    expect(quoteTag({ kind: 'selection', text, turn: 7, start: 210, end: 241 }, turnNo)).toEqual({ short: false, label: 'R3 #211–241' });
  });

  it('shortens a passage over more than one line however short', () => {
    expect(quoteTag({ kind: 'selection', text: '第一行\n第二行', turn: 7, start: 0, end: 7 }, turnNo)).toMatchObject({ short: false });
  });

  it('shortens a passage to its start when where it came from is unknown', () => {
    expect(quoteTag({ kind: 'selection', text: '这是一段很长的回复内容，超过了直接显示的长度' }, turnNo)).toEqual({ short: false, label: '「这是一段很长的…」' });
    expect(quoteTag({ kind: 'selection', text: '这是一段很长的回复内容，超过了直接显示的长度', turn: 99, start: 0, end: 22 }, turnNo).label).toBe('「这是一段很长的…」');
  });
});
