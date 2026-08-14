import { Component } from 'react';
import ErrorState from './ErrorState';

// Keeps one broken screen from blanking the whole app. Reset it by keying the
// element on the route, so navigating away clears the error.
export default class ErrorBoundary extends Component {
  state = { error: null };

  static getDerivedStateFromError(error) {
    return { error };
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="page">
        {/* Reload rather than clearing state: React.lazy caches a rejected
            import, so a failed chunk would re-throw immediately and the retry
            button would do nothing. */}
        <ErrorState
          title="This screen ran into a problem"
          text="Something unexpected happened while rendering."
          onRetry={() => window.location.reload()}
        />
      </div>
    );
  }
}
