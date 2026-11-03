import { describe, expect, it } from 'vitest';
import { parsePatch } from './diff';

const patch = `diff --git a/api/limit.go b/api/limit.go
index 3b18e51..a1c2f3d 100644
--- a/api/limit.go
+++ b/api/limit.go
@@ -1,4 +1,5 @@ package api
 package api
 
-const limit = 10
+const limit = 50
+const burst = 5
 
@@ -20,2 +21,2 @@ func Allow() bool {
-	return n < limit
+	return n <= limit
\\ No newline at end of file
`;

describe('parsePatch', () => {
  it('numbers each line in the old and the new file', () => {
    const d = parsePatch(patch);
    expect(d.binary).toBe(false);
    expect(d.hunks).toHaveLength(2);
    const [first, second] = d.hunks;
    expect(first!.context).toBe('package api');
    expect(first!.lines.map((l) => [l.kind, l.old, l.new])).toEqual([
      ['context', 1, 1],
      ['context', 2, 2],
      ['del', 3, undefined],
      ['add', undefined, 3],
      ['add', undefined, 4],
      ['context', 4, 5],
    ]);
    expect(second!.lines.map((l) => [l.kind, l.old, l.new, l.text])).toEqual([
      ['del', 20, undefined, '\treturn n < limit'],
      ['add', undefined, 21, '\treturn n <= limit'],
      ['note', undefined, undefined, 'No newline at end of file'],
    ]);
  });

  it('says when a file is binary, and keeps renames and modes', () => {
    const d = parsePatch(
      'diff --git a/logo.png b/img/logo.png\nsimilarity index 100%\nrename from logo.png\nrename to img/logo.png\nBinary files a/logo.png and b/img/logo.png differ\n',
    );
    expect(d.binary).toBe(true);
    expect(d.hunks).toEqual([]);
    expect(d.notes).toEqual(['similarity index 100%', 'rename from logo.png', 'rename to img/logo.png']);
  });
});
