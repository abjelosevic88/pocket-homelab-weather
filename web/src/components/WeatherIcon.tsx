// The weather of an hour or a day as a small coloured picture: sun, moon,
// clouds, rain, snow, fog and lightning, drawn inline (32-grid).
import type { Condition } from '../lib/types'

const CLOUD = 'M9 25h14.5a5.5 5.5 0 0 0 .6-10.97A7.5 7.5 0 0 0 9.6 15.1 5 5 0 0 0 9 25z'
const SMALL_CLOUD = 'M12 27h11a4.5 4.5 0 0 0 .5-8.97A6.2 6.2 0 0 0 11.6 18.9 4.1 4.1 0 0 0 12 27z'

function Sun({ cx = 16, cy = 16, r = 6.5, rays = true }: { cx?: number; cy?: number; r?: number; rays?: boolean }) {
  const ray = (a: number) => {
    const rad = (a * Math.PI) / 180
    const x1 = cx + Math.cos(rad) * (r + 2.6), y1 = cy + Math.sin(rad) * (r + 2.6)
    const x2 = cx + Math.cos(rad) * (r + 5.4), y2 = cy + Math.sin(rad) * (r + 5.4)
    return <line key={a} x1={x1} y1={y1} x2={x2} y2={y2} />
  }
  return <g className="wx-sun"><circle cx={cx} cy={cy} r={r} />{rays && <g className="wx-rays">{[0, 45, 90, 135, 180, 225, 270, 315].map(ray)}</g>}</g>
}

function Moon({ x = 0, y = 0, s = 1 }: { x?: number; y?: number; s?: number }) {
  return <path className="wx-moon" transform={`translate(${x} ${y}) scale(${s})`} d="M20.5 19.5A9 9 0 0 1 12.5 6.6a9 9 0 1 0 12 12.2 9 9 0 0 1-4 .7z" />
}

export function WeatherIcon({ condition, day = true, size = 32, className = '', title }: { condition: Condition | string; day?: boolean; size?: number; className?: string; title?: string }) {
  const c = condition as Condition
  let body: React.ReactNode
  const drops = (n: number, small = false) => (
    <g className="wx-drop">{[10, 15.5, 21].slice(0, n).map((x, i) => <line key={x} x1={x + 1} y1={small ? 27 : 26.5 + (i % 2)} x2={x - 0.5} y2={small ? 29.5 : 30.5 + (i % 2)} />)}</g>
  )
  const flakes = <g className="wx-flake">{[10.5, 16, 21.5].map((x, i) => <circle key={x} cx={x} cy={28 + (i % 2) * 1.5} r={1.3} />)}</g>
  switch (c) {
    case 'clear':
      body = day ? <Sun /> : <Moon x={-2} y={-1} s={1.05} />
      break
    case 'mostly-clear':
    case 'partly':
      body = <>{day ? <Sun cx={12} cy={12} r={5.2} /> : <Moon x={-5} y={-5} s={0.85} />}<path className="wx-cloud" d={c === 'partly' ? SMALL_CLOUD : 'M15 27h9a3.6 3.6 0 0 0 .4-7.2 5 5 0 0 0-9.6.9A3.2 3.2 0 0 0 15 27z'} /></>
      break
    case 'cloudy':
      body = <><path className="wx-cloud back" d="M14 16h9a4.5 4.5 0 0 0 .4-9 6.2 6.2 0 0 0-12 .8A3.7 3.7 0 0 0 14 16z" /><path className="wx-cloud" d={CLOUD} transform="translate(0 2)" /></>
      break
    case 'fog':
      body = <><path className="wx-cloud back" d={CLOUD} transform="translate(0 -4)" /><g className="wx-fog"><line x1="5" y1="23" x2="27" y2="23" /><line x1="8" y1="27" x2="24" y2="27" /></g></>
      break
    case 'drizzle':
      body = <><path className="wx-cloud" d={CLOUD} transform="translate(0 -4)" />{drops(2, true)}</>
      break
    case 'rain':
      body = <><path className="wx-cloud" d={CLOUD} transform="translate(0 -4)" />{drops(3)}</>
      break
    case 'freezing':
      body = <><path className="wx-cloud" d={CLOUD} transform="translate(0 -4)" /><g className="wx-drop"><line x1="11" y1="26.5" x2="9.5" y2="30.5" /><line x1="22" y1="26.5" x2="20.5" y2="30.5" /></g><g className="wx-flake"><circle cx="16" cy="28.5" r="1.4" /></g></>
      break
    case 'snow':
      body = <><path className="wx-cloud" d={CLOUD} transform="translate(0 -4)" />{flakes}</>
      break
    case 'storm':
      body = <><path className="wx-cloud dark" d={CLOUD} transform="translate(0 -5)" /><path className="wx-bolt" d="M17.5 18.5 13 25h3.4l-1.6 6 5.4-7.6h-3.5l1.8-4.9z" /></>
      break
    default:
      body = <path className="wx-cloud" d={CLOUD} />
  }
  return (
    <svg className={`wx-ico ${className}`} width={size} height={size} viewBox="0 0 32 32" aria-hidden={title ? undefined : true} role={title ? 'img' : undefined}>
      {title && <title>{title}</title>}
      {body}
    </svg>
  )
}
