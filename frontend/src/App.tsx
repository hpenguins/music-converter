import { useState, useCallback, useEffect } from 'react';
import './App.css';
import {
  SelectNCMFiles, SelectOutputDir, GetDefaultOutputDir,
  ConvertFiles, GetCoverAsBase64, GetSettings, SaveSettings, CheckFFmpeg,
} from "../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../wailsjs/runtime";

// ======================== Types ========================

interface AppSettings {
  saveCoverFile: boolean;
}

interface FileItem {
  path: string;
  name: string;
  size: number;
}

interface OutputItem {
  fileName: string;
  inputPath: string;
  output: string;
  title: string;
  artist: string;
  album: string;
  format: string;
  coverPath: string;
  coverDataUrl: string;
}

interface ProgressEvent {
  current: number;
  total: number;
  fileName: string;
  status: string; // "decrypting" | "transcoding" | "success" | "error"
  error?: string;
  output?: string;
  title?: string;
  artist?: string;
  album?: string;
  format?: string;
  coverPath?: string;
  transPct?: number;
}

const FORMATS = ['auto', 'mp3', 'flac', 'ogg', 'wav'] as const;
type AudioFormat = typeof FORMATS[number];

function formatSize(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + ['B', 'KB', 'MB', 'GB'][i];
}

function basename(p: string): string {
  const parts = p.replace(/\\/g, '/').split('/');
  return parts[parts.length - 1] || p;
}

// ======================== App ========================

function App() {
  // --- state ---
  const [inputs, setInputs] = useState<FileItem[]>([]);
  const [formatMap, setFormatMap] = useState<Map<string, AudioFormat>>(new Map());
  const [outputs, setOutputs] = useState<OutputItem[]>([]);
  const [outputDir, setOutputDir] = useState('');
  const [isProcessing, setIsProcessing] = useState(false);
  const [progress, setProgress] = useState({ current: 0, total: 0 });
  const [dragOver, setDragOver] = useState(false);
  const [selectedOutput, setSelectedOutput] = useState<OutputItem | null>(null);
  const [coverLoading, setCoverLoading] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [settings, setSettings] = useState<AppSettings>({ saveCoverFile: true });
  const [ffmpegAvail, setFfmpegAvail] = useState(true);

  // --- load settings & check ffmpeg ---
  useEffect(() => {
    GetSettings().then(s => { if (s) setSettings(s); });
    CheckFFmpeg().then(ok => setFfmpegAvail(ok));
  }, []);

  // --- events ---
  useEffect(() => {
    const h = (d: ProgressEvent) => {
      setProgress({ current: d.current, total: d.total });

      if (d.status === 'success' && d.output) {
        const item: OutputItem = {
          fileName: basename(d.output),
          inputPath: '',
          output: d.output,
          title: d.title || '',
          artist: d.artist || '',
          album: d.album || '',
          format: d.format || '',
          coverPath: d.coverPath || '',
          coverDataUrl: '',
        };
        const input = inputs.find(f => f.name === d.fileName);
        if (input) item.inputPath = input.path;

        setOutputs(prev => {
          if (prev.find(o => o.output === d.output)) return prev;
          return [...prev, item];
        });

        setInputs(prev => prev.filter(f => f.name !== d.fileName));
        setFormatMap(prev => { const m = new Map(prev); m.delete(d.fileName); return m; });
      }
    };
    EventsOn('convert:progress', h);
    return () => { EventsOff('convert:progress'); };
  }, [inputs]);

  // drag-drop
  useEffect(() => {
    const h = (paths: string[]) => {
      if (!paths || paths.length === 0) return;
      const audioFiles = paths
        .filter((p: string) => {
          const ext = p.toLowerCase().slice(p.lastIndexOf('.'));
          return ['.ncm', '.mp3', '.flac', '.ogg', '.wav', '.m4a', '.wma', '.aac', '.opus'].includes(ext);
        })
        .map((p: string) => ({ path: p, name: basename(p), size: 0 }));
      if (audioFiles.length === 0) return;
      setInputs(prev => {
        const exist = new Set(prev.map(f => f.path));
        const fresh = audioFiles.filter(f => !exist.has(f.path));
        return [...prev, ...fresh];
      });
    };
    EventsOn('wails:dragdrop', h);
    return () => { EventsOff('wails:dragdrop'); };
  }, []);

  // default output dir
  useEffect(() => {
    GetDefaultOutputDir().then(d => { if (d) setOutputDir(d); });
  }, []);

  // --- toggle cover file setting ---
  const toggleCoverFile = useCallback(async () => {
    const next = { ...settings, saveCoverFile: !settings.saveCoverFile };
    setSettings(next);
    try { await SaveSettings(next); } catch { /* ignore */ }
  }, [settings]);

  // --- handlers ---
  const addFiles = useCallback(async () => {
    if (isProcessing) return;
    try {
      const sel = await SelectNCMFiles();
      if (!sel || sel.length === 0) return;
      setInputs(prev => {
        const exist = new Set(prev.map(f => f.path));
        const fresh = sel.filter(f => !exist.has(f.path));
        return [...prev, ...fresh];
      });
    } catch { /* ignore */ }
  }, [isProcessing]);

  const pickOutputDir = useCallback(async () => {
    if (isProcessing) return;
    try {
      const d = await SelectOutputDir();
      if (d) setOutputDir(d);
    } catch { /* ignore */ }
  }, [isProcessing]);

  const removeInput = useCallback((path: string) => {
    if (isProcessing) return;
    const f = inputs.find(i => i.path === path);
    setInputs(prev => prev.filter(i => i.path !== path));
    if (f) setFormatMap(prev => { const m = new Map(prev); m.delete(f.name); return m; });
  }, [isProcessing, inputs]);

  const clearInputs = useCallback(() => {
    if (isProcessing) return;
    setInputs([]);
    setFormatMap(new Map());
  }, [isProcessing]);

  const clearOutputs = useCallback(() => {
    setOutputs([]);
    setSelectedOutput(null);
  }, []);

  const setFileFormat = useCallback((fileName: string, fmt: AudioFormat) => {
    setFormatMap(prev => { const m = new Map(prev); m.set(fileName, fmt); return m; });
  }, []);

  // --- convert ---
  const startConvert = useCallback(async () => {
    if (isProcessing || inputs.length === 0 || !outputDir) return;
    setIsProcessing(true);
    setOutputs([]);
    setSelectedOutput(null);
    setProgress({ current: 0, total: inputs.length });

    const requests = inputs.map(f => ({
      path: f.path,
      format: formatMap.get(f.name) || 'auto',
    }));

    try {
      await ConvertFiles(requests, outputDir);
    } catch (err) {
      console.error(err);
    } finally {
      setIsProcessing(false);
    }
  }, [isProcessing, inputs, outputDir, formatMap]);

  // --- click output item for detail ---
  const showDetail = useCallback(async (item: OutputItem) => {
    setCoverLoading(true);
    let cover = '';
    if (item.coverPath) {
      try { cover = await GetCoverAsBase64(item.coverPath); } catch { /* ignore */ }
    }
    setSelectedOutput({ ...item, coverDataUrl: cover });
    setCoverLoading(false);
  }, []);

  // --- stats ---
  const pct = progress.total > 0 ? Math.round((progress.current / progress.total) * 100) : 0;
  const processingName = isProcessing && progress.total > 0
    ? (() => {
        const idx = Math.min(progress.current, inputs.length - 1);
        return idx >= 0 ? inputs[idx]?.name : '';
      })()
    : '';

  return (
    <div id="App">
      {/* ====== Header ====== */}
      <header className="app-header">
        <div className="header-icon">Audio Converter</div>
        <div className="header-toolbar">
          <button className="btn btn-primary" onClick={addFiles} disabled={isProcessing}>
            Select Files
          </button>
          <div className="output-dir">
            <span className="dir-label">Output:</span>
            <span className="dir-path" title={outputDir}>{outputDir || '(not set)'}</span>
            <button className="btn btn-small" onClick={pickOutputDir} disabled={isProcessing}>Browse</button>
          </div>
          <button className="btn btn-icon" onClick={() => setShowSettings(!showSettings)} title="Settings">
            &#9881;
          </button>
        </div>
      </header>

      {/* ====== Settings panel ====== */}
      {showSettings && (
        <div className="settings-bar">
          <label className="settings-item">
            <span className="settings-label">Save cover files</span>
            <span className="settings-desc">Write cover images as separate .jpg files alongside audio output</span>
            <div className="toggle-wrapper">
              <input type="checkbox" className="toggle-input" id="saveCover"
                checked={settings.saveCoverFile}
                onChange={toggleCoverFile}
              />
              <label className="toggle-track" htmlFor="saveCover">
                <span className="toggle-knob" />
              </label>
            </div>
          </label>
        </div>
      )}

      {/* ====== No-FFmpeg notice ====== */}
      {!ffmpegAvail && (
        <div className="notice-bar">
          <span className="notice-icon">&#9432;</span>
          <span>FFmpeg not found &mdash; transcoding disabled. Only decryption with AUTO format is available. Place ffmpeg.exe in PATH or app directory.</span>
        </div>
      )}

      {/* ====== Body ====== */}
      <div className="app-body"
        onDragOver={e => { e.preventDefault(); setDragOver(true); }}
        onDragLeave={e => { e.preventDefault(); setDragOver(false); }}
        onDrop={e => { e.preventDefault(); setDragOver(false); }}
      >
        {/* ====== Left panel: inputs ====== */}
        <section className={`panel panel-left ${dragOver ? 'drag-over' : ''}`}>
          <div className="panel-header">
            <h2 className="panel-title">Files to Process</h2>
            <span className="panel-count">{inputs.length}</span>
          </div>

          {inputs.length === 0 ? (
            <div className="panel-placeholder">
              <p>Drop audio files here</p>
              <p className="hint">or click Select Files above</p>
            </div>
          ) : (
            <div className="panel-list">
              {inputs.map(f => (
                <div key={f.path} className="input-row">
                  <div className="input-row-info">
                    <span className="file-name">{f.name}</span>
                    <span className="file-size">{f.size > 0 ? formatSize(f.size) : ''}</span>
                  </div>
                  <div className="input-row-actions">
                    {ffmpegAvail ? (
                      <select
                        className="format-select"
                        value={formatMap.get(f.name) || 'auto'}
                        onChange={e => setFileFormat(f.name, e.target.value as AudioFormat)}
                        disabled={isProcessing}
                      >
                        {FORMATS.map(fmt => (
                          <option key={fmt} value={fmt}>{fmt.toUpperCase()}</option>
                        ))}
                      </select>
                    ) : (
                      <span className="format-auto">AUTO</span>
                    )}
                    <button
                      className="btn-icon"
                      onClick={() => removeInput(f.path)}
                      disabled={isProcessing}
                      title="Remove"
                    >&times;</button>
                  </div>
                </div>
              ))}
            </div>
          )}

          {inputs.length > 0 && (
            <div className="panel-footer">
              <button className="btn btn-ghost btn-small" onClick={clearInputs} disabled={isProcessing}>
                Clear All
              </button>
            </div>
          )}
        </section>

        {/* ====== Center: convert button ====== */}
        <section className="panel-center">
          <button
            className={`btn-convert ${isProcessing ? 'processing' : ''}`}
            onClick={startConvert}
            disabled={isProcessing || inputs.length === 0 || !outputDir}
          >
            <span className="convert-arrow">{isProcessing ? '' : '▶'}</span>
            <span>{isProcessing ? 'Converting...' : 'Start Convert'}</span>
          </button>

          {isProcessing && (
            <div className="center-progress">
              <div className="progress-bar-vert">
                <div className="progress-fill-vert" style={{ height: `${pct}%` }} />
              </div>
              <span className="progress-label">{pct}%</span>
              <span className="progress-detail">{progress.current} / {progress.total}</span>
            </div>
          )}
        </section>

        {/* ====== Right panel: outputs ====== */}
        <section className="panel panel-right">
          <div className="panel-header">
            <h2 className="panel-title">Completed</h2>
            <span className="panel-count">{outputs.length}</span>
          </div>

          {outputs.length === 0 ? (
            <div className="panel-placeholder">
              <p>{isProcessing ? 'Converting...' : 'No files converted yet'}</p>
            </div>
          ) : (
            <div className="panel-list">
              {outputs.map(o => (
                <div
                  key={o.output}
                  className={`output-row ${selectedOutput?.output === o.output ? 'active' : ''}`}
                  onClick={() => showDetail(o)}
                >
                  <div className="output-row-icon">&#10003;</div>
                  <div className="output-row-info">
                    <span className="file-name">{basename(o.output)}</span>
                    {(o.title || o.artist) && (
                      <span className="file-meta">
                        {o.title}{o.artist ? ' — ' + o.artist : ''}
                      </span>
                    )}
                  </div>
                  <span className="output-format">{o.format.toUpperCase()}</span>
                </div>
              ))}
            </div>
          )}

          {outputs.length > 0 && (
            <div className="panel-footer">
              <button className="btn btn-ghost btn-small" onClick={clearOutputs}>
                Clear All
              </button>
            </div>
          )}
        </section>
      </div>

      {/* ====== Detail bar ====== */}
      {selectedOutput && (
        <div className="detail-bar">
          <div className="detail-inner">
            <div className="detail-cover">
              {coverLoading ? (
                <div className="cover-ph loading">Loading...</div>
              ) : selectedOutput.coverDataUrl ? (
                <img className="cover-ph" src={selectedOutput.coverDataUrl} alt="cover" />
              ) : (
                <div className="cover-ph empty">No Cover</div>
              )}
            </div>
            <div className="detail-meta">
              <div className="detail-field">
                <span className="detail-label">Title</span>
                <span className="detail-value">{selectedOutput.title || '(unknown)'}</span>
              </div>
              <div className="detail-field">
                <span className="detail-label">Artist</span>
                <span className="detail-value">{selectedOutput.artist || '(unknown)'}</span>
              </div>
              <div className="detail-field">
                <span className="detail-label">Album</span>
                <span className="detail-value">{selectedOutput.album || '(unknown)'}</span>
              </div>
              <div className="detail-field">
                <span className="detail-label">Format</span>
                <span className="detail-value fmt">{selectedOutput.format.toUpperCase()}</span>
              </div>
              <div className="detail-field file-info">
                <span className="detail-label">File</span>
                <span className="detail-value path" title={selectedOutput.output}>
                  {basename(selectedOutput.output)}
                </span>
              </div>
            </div>
            <button className="btn-close-detail" onClick={() => setSelectedOutput(null)}>&times;</button>
          </div>
        </div>
      )}

      {/* ====== Footer ====== */}
      <footer className="app-footer">
        <span>Drop audio files onto the window</span>
        {processingName && <span className="now-processing">Now: {processingName}</span>}
        <span className="footer-dir" title={outputDir}>{outputDir}</span>
      </footer>
    </div>
  );
}

export default App;
