import { Component, type ErrorInfo, type ReactNode } from 'react'
import { ErrorState } from './ui/States'

type Props = { children: ReactNode }
type State = { error: Error | null }

/**
 * Catches render errors so a single bad field cannot blank the whole app.
 * React unmounts the tree on an uncaught render error, which produces a white
 * page with no explanation for the user; this boundary shows what happened and
 * lets them continue to another page.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Kept on the console rather than shipped anywhere: no telemetry endpoint
    // is configured, and user data must not leave the machine.
    console.error('Unhandled render error', error, info.componentStack)
  }

  private reset = () => this.setState({ error: null })

  render() {
    if (this.state.error) {
      return (
        <div className="p-6">
          <ErrorState
            message={`Something went wrong rendering this page: ${this.state.error.message}`}
            onRetry={this.reset}
          />
        </div>
      )
    }
    return this.props.children
  }
}
