export default function PageHeading({ title, description }) {
  return (
    <div className="mb-6 border-b border-border pb-5">
      <div className="mb-1.5 font-mono text-xs tracking-widest text-primary uppercase">
        CertHealthz
      </div>
      <h1 className="font-heading text-2xl font-medium tracking-tight">{title}</h1>
      {description && (
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{description}</p>
      )}
    </div>
  );
}
