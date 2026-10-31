import { Component, type ReactNode } from 'react'
import { SystemService } from '../../bindings/larvyn'

/** Keeps one broken view from blanking the whole window. */
export class ErrorBoundary extends Component<{ children: ReactNode; resetKey?: string }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  componentDidCatch(error: Error) {
    SystemService.Log('error', 'view crashed: ' + (error.stack ?? error.message))
  }
  componentDidUpdate(prev: { resetKey?: string }) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) this.setState({ error: null })
  }
  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="view pad">
        <div className="error-box">
          <b>This view crashed</b>
          {'\n'}
          {this.state.error.message}
          {'\n\n'}
          <span className="mono" style={{ fontSize: 11, opacity: 0.8 }}>
            {this.state.error.stack?.split('\n').slice(0, 6).join('\n')}
          </span>
        </div>
        <button className="btn" style={{ marginTop: 12 }} onClick={() => this.setState({ error: null })}>
          Try again
        </button>
      </div>
    )
  }
}
