import json
from pathlib import Path
base = Path(__file__).resolve().parent
source = (base / 'pelican-original.html').read_text()
edits = []
def edit(old, new):
    global source
    assert source.count(old) == 1, old
    edits.append(dict(old_text=old, new_text=new))
    source = source.replace(old, new, 1)
edit('一只鹈鹕，一辆薄荷绿自行车，还有刚刚好的海风。', '鹈鹕会眨眼，围巾追着海风跑。试试强海风，让这段骑行更有活力。')
edit('<circle cx="795" cy="112" r="59"', '<circle id="sunHalo" cx="795" cy="112" r="59"')
eye = '<circle cx="514" cy="129" r="9" fill="#e8dcac"/><circle cx="516" cy="129" r="4.5" fill="#263c3b"/><circle cx="517.5" cy="127.5" r="1.4" fill="white"/>'
edit(eye, '<g id="eye">'+eye+'</g>')
edit('        <g id="gulls"', '        <g id="windLines" fill="none" stroke="#f8f8eb" stroke-width="3" stroke-linecap="round" opacity=".25"><path d="M130 190h60m-95 27h34M727 260h78m-42 20h38M230 337h52"/></g>\n        <g id="gulls"')
edit('        <label class="speed"', '        <button id="wind" type="button" aria-pressed="false">≈ 轻海风</button>\n        <label class="speed"')
edit('let running = !reducedMotion.matches, speed = 1, t = 0, last = null;', 'let running = !reducedMotion.matches, speed = 1, wind = 1, t = 0, last = null;')
edit("$('statusText').textContent = running ? '海风正好，继续向前' : '停一会儿，看看海';", "$('statusText').textContent = running ? (wind > 1 ? '迎着海风，围巾飞起来了' : '海风正好，继续向前') : '停一会儿，看看海';\n      $('wind').textContent = wind > 1 ? '≈ 强海风' : '≈ 轻海风';\n      $('wind').setAttribute('aria-pressed', String(wind > 1));")
edit("    $('speed').addEventListener('input'", "    $('wind').addEventListener('click', () => {wind = wind === 1 ? 2.5 : 1; updateControls(); render();});\n    $('speed').addEventListener('input'")
edit('const a = t*2.6, bob = Math.sin(a*2)*2.2;', 'const a = t*2.6, bob = Math.sin(a*2)*(2.2 + (wind-1)*1.2);')
edit("$('scarfTail').setAttribute('transform',`rotate(${Math.sin(t*5)*3} 483 196)`);", "$('scarfTail').setAttribute('transform',`rotate(${Math.sin(t*5)*5*wind - (wind-1)*4} 483 196)`);\n      const blinkPhase = t % 5.2;\n      const eyeOpen = Math.max(.06, 1 - Math.max(0, 1-Math.abs(blinkPhase-4.8)/.14));\n      $('eye').setAttribute('transform', `translate(516 129) scale(1 ${eyeOpen}) translate(-516 -129)`);\n      $('sunHalo').setAttribute('r', String(59 + Math.sin(t*.9)*4));\n      $('sunHalo').setAttribute('stroke-opacity', String(.3 + Math.sin(t*.9)*.12));\n      $('windLines').setAttribute('transform', `translate(${-t*32*wind%120} 0)`);\n      $('windLines').setAttribute('opacity', String(.16 + wind*.12));")
edit('translate(${-t*7%1050} 0)', 'translate(${-t*7*wind%1050} 0)')
edit('translate(0 ${Math.sin(t*.8)*6})', 'translate(${Math.sin(t*.5)*12*wind} ${Math.sin(t*.8)*6*wind})')
(base / 'pelican-edits.json').write_text(json.dumps(edits, ensure_ascii=False, indent=2)+'\n')
(base / 'pelican-preview.html').write_text(source)
print(f'Prepared {len(edits)} unique edits')
