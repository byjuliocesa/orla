import { describe, expect, it } from 'vitest';
import { parsePrompt } from './prompt';

describe('parsePrompt', () => {
  it('reads back the attachments and the ultracode keyword', () => {
    const p = parsePrompt(
      'look at this\n\nAttached files:\n- /home/a/.local/share/orla/uploads/upl_460ed13ebee43e11fb1202a7/image.png\n- /x/uploads/upl_000000000000000000000001/main.go\n\nultracode',
    );
    expect(p.text).toBe('look at this');
    expect(p.ultracode).toBe(true);
    expect(p.files.map((f) => [f.id, f.name])).toEqual([
      ['upl_460ed13ebee43e11fb1202a7', 'image.png'],
      ['upl_000000000000000000000001', 'main.go'],
    ]);
  });

  it('leaves a prompt that only mentions the words alone', () => {
    const p = parsePrompt('write "Attached files:" in the README');
    expect(p).toEqual({ text: 'write "Attached files:" in the README', files: [], ultracode: false });
  });
});
