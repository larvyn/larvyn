import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { create } from 'zustand'
import {
  Activity, Bike, BookOpen, Bot, Briefcase, Building2, Calendar, Camera, Car, ChartLine, Cloud, Code, Coffee, Cpu,
  CreditCard, Database, Dumbbell, FileText, Flame, Gamepad2, Gem, Gift, Globe, GraduationCap, Hammer, Heart, House,
  Leaf, Lightbulb, Lock, Mail, MapPin, MessageCircle, Music, Newspaper, Package, Palette, PawPrint, Plane, Rocket,
  Server, Settings2, Shield, ShoppingCart, Smartphone, Sparkles, Star, Store, Ticket, Truck, Users, Utensils, Wallet,
  Wrench, Zap, GitBranch, ImageUp, Trash2,
} from 'lucide-react'
import type { Project } from '../../bindings/larvyn/internal/store'
import { ProjectService, type ProjectImages } from '../../bindings/larvyn/internal/project'
import { fail, toast } from '../lib/store'
import { cx, errMsg, initials } from '../lib/util'
import { Modal } from './ui'

/** The icons a project badge can show, keyed by the name saved in the project. */
export const PROJECT_ICONS: Record<string, typeof Rocket> = {
  rocket: Rocket, 'shopping-cart': ShoppingCart, store: Store, 'credit-card': CreditCard, wallet: Wallet,
  briefcase: Briefcase, 'building-2': Building2, users: Users, 'message-circle': MessageCircle, mail: Mail,
  calendar: Calendar, ticket: Ticket, 'chart-line': ChartLine, activity: Activity, database: Database,
  server: Server, cloud: Cloud, cpu: Cpu, code: Code, bot: Bot, 'settings-2': Settings2, wrench: Wrench,
  hammer: Hammer, shield: Shield, lock: Lock, globe: Globe, 'map-pin': MapPin, plane: Plane, car: Car,
  truck: Truck, bike: Bike, house: House, utensils: Utensils, coffee: Coffee, 'graduation-cap': GraduationCap,
  'book-open': BookOpen, newspaper: Newspaper, 'file-text': FileText, camera: Camera, music: Music,
  'gamepad-2': Gamepad2, palette: Palette, heart: Heart, dumbbell: Dumbbell, 'paw-print': PawPrint, leaf: Leaf,
  flame: Flame, zap: Zap, star: Star, sparkles: Sparkles, gem: Gem, gift: Gift, lightbulb: Lightbulb,
  package: Package, smartphone: Smartphone,
}

export const PROJECT_COLORS = [
  '#FF2D20', '#F97316', '#EAB308', '#84CC16', '#22C55E', '#14B8A6', '#06B6D4', '#3B82F6',
  '#6366F1', '#8B5CF6', '#D946EF', '#EC4899', '#F43F5E', '#78716C', '#64748B',
]

/** Dark text on light backgrounds, white on the rest. */
function ink(hex: string) {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex ?? '')
  if (!m) return '#fff'
  const n = parseInt(m[1], 16)
  const [r, g, b] = [n >> 16, (n >> 8) & 255, n & 255].map((c) => {
    const v = c / 255
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.45 ? '#18181b' : '#fff'
}

/** A project's color as the inline style of its badge. */
export function badgeStyle(p: Pick<Project, 'color'>): CSSProperties {
  return { background: p.color, color: ink(p.color) }
}

// ---- images ----------------------------------------------------------------

/**
 * The pictures of each project (GitHub avatar, custom image), loaded once per
 * project. The git avatar may take a download, so badges show the color and
 * initials until it arrives.
 */
const useImages = create<Record<string, ProjectImages>>(() => ({}))
const loading = new Set<string>()

export function loadImages(id: string, force = false) {
  if (!id || (!force && (loading.has(id) || useImages.getState()[id]))) return
  loading.add(id)
  ProjectService.Images(id)
    .then((img) => useImages.setState({ [id]: img }))
    .catch(() => {})
    .finally(() => loading.delete(id))
}

/** The image a project's badge shows, if its icon setting uses one. */
function useBadgeImage(p: Pick<Project, 'id' | 'icon'>) {
  const img = useImages((s) => s[p.id])
  useEffect(() => loadImages(p.id), [p.id])
  const icon = p.icon ?? ''
  if (icon === 'custom') return img?.custom || undefined
  if (icon === '') return img?.git || undefined
  return undefined
}

type BadgeProject = Pick<Project, 'id' | 'name' | 'icon' | 'color'>

/**
 * What goes inside a project's badge: its image, its icon, or its initials.
 * `size` is the icon size in px; `src` overrides the image (dialog preview).
 */
export function BadgeContent({ project, size, src }: { project: BadgeProject; size: number; src?: string }) {
  const image = useBadgeImage(project)
  const shown = src ?? image
  if (shown) return <img className="badge-img" src={shown} alt="" draggable={false} />
  const Icon = project.icon ? PROJECT_ICONS[project.icon] : undefined
  return Icon ? <Icon style={{ width: size, height: size }} strokeWidth={2.2} /> : <>{initials(project.name)}</>
}

/** A small square badge with the project's color and image, icon or initials. */
export function ProjectBadge({ project, size = 18, className, src }: { project: BadgeProject; size?: number; className?: string; src?: string }) {
  return (
    <span
      className={cx('project-badge', className)}
      style={{ ...badgeStyle(project), width: size, height: size, borderRadius: Math.round(size * 0.28), fontSize: Math.max(7.5, size * 0.42) }}
    >
      <BadgeContent project={project} size={Math.round(size * 0.58)} src={src} />
    </span>
  )
}

/** Reads an image file and turns it into a square 128px PNG data URL. */
function toSquarePng(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const img = new Image()
    img.onload = () => {
      const n = 128
      const c = document.createElement('canvas')
      c.width = c.height = n
      const ctx = c.getContext('2d')!
      const w = img.naturalWidth || n
      const h = img.naturalHeight || n
      // Cover: fill the square and crop the longer side.
      const k = Math.max(n / w, n / h)
      ctx.imageSmoothingQuality = 'high'
      ctx.drawImage(img, (n - w * k) / 2, (n - h * k) / 2, w * k, h * k)
      URL.revokeObjectURL(url)
      resolve(c.toDataURL('image/png'))
    }
    img.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error('That file is not an image Larvyn can read'))
    }
    img.src = url
  })
}

// ---- customize -------------------------------------------------------------

const useCustomize = create<{ project: Project | null }>(() => ({ project: null }))

/** Opens the dialog that changes a project's name, color and icon. */
export function customizeProject(p: Project) {
  useCustomize.setState({ project: p })
}

export function CustomizeProjectHost() {
  const project = useCustomize((s) => s.project)
  const images = useImages((s) => (project ? s[project.id] : undefined))
  const [name, setName] = useState('')
  const [color, setColor] = useState('')
  const [icon, setIcon] = useState('')
  // The custom image as it will be saved: undefined = unchanged, '' = removed.
  const [custom, setCustom] = useState<string | undefined>(undefined)
  const [saving, setSaving] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (!project) return
    setName(project.name)
    setColor(project.color)
    setIcon(project.icon ?? '')
    setCustom(undefined)
    loadImages(project.id, true)
  }, [project])
  if (!project) return null
  const close = () => useCustomize.setState({ project: null })
  const customSrc = custom ?? images?.custom ?? ''
  const save = async () => {
    setSaving(true)
    try {
      if (custom !== undefined) await ProjectService.SetCustomIcon(project.id, custom)
      // Without an image "custom" would show the initials: say so.
      const finalIcon = icon === 'custom' && !customSrc ? 'initials' : icon
      await ProjectService.Update({ ...project, name: name.trim() || project.name, color, icon: finalIcon })
      if (custom !== undefined) loadImages(project.id, true)
      close()
    } catch (e) {
      fail('Could not save the project')(e)
    } finally {
      setSaving(false)
    }
  }
  const pick = async (file?: File) => {
    if (!file) return
    try {
      setCustom(await toSquarePng(file))
      setIcon('custom')
    } catch (e) {
      toast('error', 'Could not use that image', errMsg(e))
    }
  }
  const preview = { id: project.id, name: name || project.name, color, icon }
  const previewSrc = icon === 'custom' ? customSrc || undefined : icon === '' ? images?.git || undefined : undefined
  return (
    <Modal
      title="Customize project"
      subtitle={project.path}
      onClose={close}
      icon={<ProjectBadge project={preview} size={34} src={previewSrc} />}
      footer={
        <>
          <button className="btn ghost" onClick={close}>
            Cancel
          </button>
          <button className="btn primary" disabled={saving} onClick={save}>
            Save
          </button>
        </>
      }
    >
      <div className="field">
        <label>Name</label>
        <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && save()} />
      </div>
      <div className="field">
        <label>Image</label>
        <div className="image-picks">
          <button
            className={cx('image-pick', icon === '' && 'on')}
            title={images?.gitOwner ? `Avatar of ${images.gitOwner} on GitHub, from the git remote` : 'Uses the avatar of the GitHub user or organization of the git remote; this project has none, so it shows the initials'}
            onClick={() => setIcon('')}
          >
            <span className="image-thumb" style={badgeStyle({ color })}>
              {images?.git ? <img className="badge-img" src={images.git} alt="" /> : <GitBranch />}
            </span>
            <span className="image-label">
              <b>{images?.gitOwner ? images.gitOwner : 'Git owner'}</b>
              <small>{images === undefined ? 'Loading…' : images.git ? 'GitHub avatar' : images.gitOwner ? 'No avatar' : 'No GitHub remote'}</small>
            </span>
          </button>
          <button
            className={cx('image-pick', icon === 'custom' && 'on')}
            title="Pick a PNG, JPG, WebP or SVG"
            onClick={() => (customSrc ? setIcon('custom') : fileRef.current?.click())}
          >
            <span className="image-thumb" style={badgeStyle({ color })}>
              {customSrc ? <img className="badge-img" src={customSrc} alt="" /> : <ImageUp />}
            </span>
            <span className="image-label">
              <b>Custom image</b>
              <small>{customSrc ? 'Your image' : 'Upload a PNG…'}</small>
            </span>
          </button>
          <div className="image-actions">
            <button className="btn sm" onClick={() => fileRef.current?.click()}>
              <ImageUp /> {customSrc ? 'Replace' : 'Upload'}
            </button>
            {customSrc && (
              <button
                className="icon-btn sm danger"
                title="Remove the custom image"
                onClick={() => {
                  setCustom('')
                  if (icon === 'custom') setIcon('')
                }}
              >
                <Trash2 />
              </button>
            )}
          </div>
          <input
            ref={fileRef}
            type="file"
            accept="image/png,image/jpeg,image/webp,image/svg+xml,image/gif"
            hidden
            onChange={(e) => {
              pick(e.target.files?.[0])
              e.target.value = ''
            }}
          />
        </div>
      </div>
      <div className="field">
        <label>Color</label>
        <div className="swatches">
          {PROJECT_COLORS.map((c) => (
            <button key={c} className={cx('swatch', c.toLowerCase() === color.toLowerCase() && 'on')} style={{ background: c }} title={c} onClick={() => setColor(c)} />
          ))}
          <label className={cx('swatch custom', !PROJECT_COLORS.some((c) => c.toLowerCase() === color.toLowerCase()) && 'on')} title="Custom color" style={{ background: color }}>
            <input type="color" value={/^#[0-9a-f]{6}$/i.test(color) ? color : '#ff2d20'} onChange={(e) => setColor(e.target.value.toUpperCase())} />
          </label>
        </div>
      </div>
      <div className="field">
        <label>Icon</label>
        <div className="icon-grid">
          <button className={cx('icon-pick', icon === 'initials' && 'on')} title="Initials" onClick={() => setIcon('initials')}>
            <span style={{ fontSize: 10.5, fontWeight: 800 }}>{initials(preview.name)}</span>
          </button>
          {Object.entries(PROJECT_ICONS).map(([key, Icon]) => (
            <button key={key} className={cx('icon-pick', icon === key && 'on')} title={key.replace(/-\d+$/, '').replace(/-/g, ' ')} onClick={() => setIcon(key)}>
              <Icon />
            </button>
          ))}
        </div>
      </div>
    </Modal>
  )
}
