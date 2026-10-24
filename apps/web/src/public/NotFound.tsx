export function NotFound(): JSX.Element {
  return (
    <div className="zn-message" role="alert">
      <h1>Not found</h1>
      <p>This page doesn’t exist or isn’t public.</p>
      <p>
        <a href="/">Go to the home page</a>
      </p>
    </div>
  )
}
