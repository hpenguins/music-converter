import {useState, useCallback, useEffect} from 'react';
import './App.css';
import {SelectNCMFiles, SelectOutputDir, GetDefaultOutputDir, DecryptFiles, GetCoverAsBase64} from "../wailsjs/go/main/App";
import {EventsOn, EventsOff} from "../wailsjs/runtime";

// ==================== 类型定义 ====================

interface FileItem {
  path: string;
  name: string;
  size: number;
}

interface DecryptStatus {
  fileName: string;
  filePath: string;
  status: string; // "processing" | "success" | "error"
  output?: string;
  error?: string;
  title?: string;
  artist?: string;
  album?: string;
  format?: string;
  coverPath?: string;
}

interface ProgressEvent {
  current: number;
  total: number;
  fileName: string;
  status: string;
  error?: string;
  output?: string;
  title?: string;
  artist?: string;
  album?: string;
  format?: string;
  coverPath?: string;
}

interface DetailInfo {
  fileName: string;
  filePath: string;
  title: string;
  artist: string;
  album: string;
  format: string;
  output: string;
  coverPath: string;
  coverDataUrl: string;
}

// ==================== 工具函数 ====================

function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function StatusIcon({status}: { status?: string }) {
  switch (status) {
    case 'processing':
      return <span className="status-icon processing">⏳</span>;
    case 'success':
      return <span className="status-icon success">✓</span>;
    case 'error':
      return <span className="status-icon error">✗</span>;
    default:
      return <span className="status-icon idle">♪</span>;
  }
}

// ==================== 主组件 ====================

function App() {
  const [files, setFiles] = useState<FileItem[]>([]);
  const [outputDir, setOutputDir] = useState('');
  const [isProcessing, setIsProcessing] = useState(false);
  const [decryptResults, setDecryptResults] = useState<Map<string, DecryptStatus>>(new Map());
  const [dragOver, setDragOver] = useState(false);
  const [overallProgress, setOverallProgress] = useState({current: 0, total: 0});
  const [detailInfo, setDetailInfo] = useState<DetailInfo | null>(null);
  const [coverLoading, setCoverLoading] = useState(false);

  // ========== 监听 Wails 进度事件 ==========
  useEffect(() => {
    const handler = (data: ProgressEvent) => {
      setOverallProgress({current: data.current, total: data.total});
      setDecryptResults(prev => {
        const next = new Map(prev);
        next.set(data.fileName, {
          fileName: data.fileName,
          filePath: '',
          status: data.status,
          output: data.output,
          error: data.error,
          title: data.title,
          artist: data.artist,
          album: data.album,
          format: data.format,
          coverPath: data.coverPath,
        });
        return next;
      });
    };

    EventsOn('decrypt:progress', handler);
    return () => { EventsOff('decrypt:progress'); };
  }, []);

  // ========== 监听 Wails 拖拽事件 ==========
  useEffect(() => {
    const dropHandler = (filePaths: string[]) => {
      if (!filePaths || filePaths.length === 0) return;
      const ncmFiles = filePaths
        .filter((p: string) => p.toLowerCase().endsWith('.ncm'))
        .map((p: string) => {
          const parts = p.replace(/\\/g, '/').split('/');
          return {
            path: p,
            name: parts[parts.length - 1] || p,
            size: 0,
          };
        });
      if (ncmFiles.length === 0) return;
      setFiles(prev => {
        const existing = new Set(prev.map(f => f.path));
        const fresh = ncmFiles.filter(f => !existing.has(f.path));
        return [...prev, ...fresh];
      });
    };

    EventsOn('wails:dragdrop', dropHandler);
    return () => { EventsOff('wails:dragdrop'); };
  }, []);

  // ========== 初始化输出目录 ==========
  useEffect(() => {
    GetDefaultOutputDir().then(dir => {
      if (dir) setOutputDir(dir);
    });
  }, []);

  // ========== 操作处理 ==========

  const handleSelectFiles = useCallback(async () => {
    if (isProcessing) return;
    try {
      const selected = await SelectNCMFiles();
      if (selected && selected.length > 0) {
        setFiles(prev => {
          const existing = new Set(prev.map(f => f.path));
          const fresh = selected.filter(f => !existing.has(f.path));
          return [...prev, ...fresh];
        });
      }
    } catch { /* user cancelled */ }
  }, [isProcessing]);

  const handleSelectOutputDir = useCallback(async () => {
    if (isProcessing) return;
    try {
      const dir = await SelectOutputDir();
      if (dir) setOutputDir(dir);
    } catch { /* user cancelled */ }
  }, [isProcessing]);

  const handleRemoveFile = useCallback((path: string) => {
    if (isProcessing) return;
    const file = files.find(f => f.path === path);
    setFiles(prev => prev.filter(f => f.path !== path));
    if (file) {
      setDecryptResults(prev => { const n = new Map(prev); n.delete(file.name); return n; });
    }
    if (detailInfo && file && detailInfo.fileName === file.name) {
      setDetailInfo(null);
    }
  }, [isProcessing, files, detailInfo]);

  const handleClearFiles = useCallback(() => {
    if (isProcessing) return;
    setFiles([]);
    setDecryptResults(new Map());
    setOverallProgress({current: 0, total: 0});
    setDetailInfo(null);
  }, [isProcessing]);

  const handleDecrypt = useCallback(async () => {
    if (isProcessing || files.length === 0 || !outputDir) return;
    setIsProcessing(true);
    setDecryptResults(new Map());
    setOverallProgress({current: 0, total: files.length});
    setDetailInfo(null);
    try {
      await DecryptFiles(files.map(f => f.path), outputDir);
    } catch (err) {
      console.error(err);
    } finally {
      setIsProcessing(false);
    }
  }, [isProcessing, files, outputDir]);

  // ========== 文件行点击 → 查看详情 ==========
  const handleRowClick = useCallback(async (file: FileItem) => {
    const st = decryptResults.get(file.name);
    if (!st || st.status !== 'success') return;

    setCoverLoading(true);

    let coverDataUrl = '';
    if (st.coverPath) {
      try {
        coverDataUrl = await GetCoverAsBase64(st.coverPath);
      } catch { /* ignore */ }
    }

    setDetailInfo({
      fileName: st.fileName || file.name,
      filePath: file.path,
      title: st.title || '',
      artist: st.artist || '',
      album: st.album || '',
      format: st.format || '',
      output: st.output || '',
      coverPath: st.coverPath || '',
      coverDataUrl,
    });
    setCoverLoading(false);
  }, [decryptResults]);

  const handleCloseDetail = useCallback(() => {
    setDetailInfo(null);
  }, []);

  // ========== 统计数据 ==========
  const successCount = Array.from(decryptResults.values()).filter(r => r.status === 'success').length;
  const errorCount = Array.from(decryptResults.values()).filter(r => r.status === 'error').length;
  const processingCount = Array.from(decryptResults.values()).filter(r => r.status === 'processing').length;
  const pct = overallProgress.total > 0
    ? Math.round((overallProgress.current / overallProgress.total) * 100)
    : 0;

  return (
    <div id="App">
      {/* ========== 标题栏 ========== */}
      <header className="app-header">
        <div className="header-icon">🎵</div>
        <div>
          <h1 className="app-title">NCM 音乐解密工具</h1>
          <p className="app-subtitle">网易云音乐 .ncm 文件 → 通用音频格式</p>
        </div>
      </header>

      <div className="app-body">
        {/* ========== 主区域 ========== */}
        <main className={`app-main ${detailInfo ? 'with-detail' : ''}`}>
          {/* 工具栏 */}
          <div className="toolbar">
            <div className="toolbar-row">
              <button className="btn btn-primary" onClick={handleSelectFiles} disabled={isProcessing}>
                📂 选择 NCM 文件
              </button>
              <div className="output-dir">
                <label className="dir-label">输出目录:</label>
                <span className="dir-path" title={outputDir}>
                  {outputDir || '未选择'}
                </span>
                <button className="btn btn-small" onClick={handleSelectOutputDir} disabled={isProcessing}>
                  浏览...
                </button>
              </div>
            </div>
          </div>

          {/* 文件列表 / 拖拽区 */}
          <div
            className={`drop-zone ${dragOver ? 'drag-over' : ''} ${files.length > 0 ? 'has-files' : ''}`}
            onDragOver={e => { e.preventDefault(); setDragOver(true); }}
            onDragLeave={e => { e.preventDefault(); setDragOver(false); }}
            onDrop={e => { e.preventDefault(); setDragOver(false); }}
          >
            {files.length === 0 ? (
              <div className="drop-placeholder">
                <div className="drop-icon">📁</div>
                <p className="drop-text">拖拽 NCM 文件到此处</p>
                <p className="drop-hint">或点击上方「选择 NCM 文件」按钮</p>
              </div>
            ) : (
              <div className="file-table-wrapper">
                <table className="file-table">
                  <thead>
                    <tr>
                      <th className="col-status"></th>
                      <th className="col-name">文件名</th>
                      <th className="col-size">大小</th>
                      <th className="col-result">状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {files.map(file => {
                      const st = decryptResults.get(file.name);
                      return (
                        <tr
                          key={file.path}
                          className={`file-row ${st?.status || ''} ${st?.status === 'success' ? 'clickable' : ''}`}
                          onClick={() => st?.status === 'success' && handleRowClick(file)}
                          title={st?.status === 'success' ? '点击查看详情' : undefined}
                        >
                          <td className="col-status"><StatusIcon status={st?.status} /></td>
                          <td className="col-name">
                            <span className="file-name">{file.name}</span>
                            {st?.title && (
                              <span className="file-meta">
                                {st.title}{st.artist ? ` — ${st.artist}` : ''}
                              </span>
                            )}
                          </td>
                          <td className="col-size">{formatFileSize(file.size)}</td>
                          <td className="col-result">
                            {!st && <span className="result-pending">等待解密</span>}
                            {st?.status === 'processing' && <span className="result-processing">解密中...</span>}
                            {st?.status === 'success' && <span className="result-success">✓ {st.format?.toUpperCase() || '成功'}</span>}
                            {st?.status === 'error' && (
                              <span className="result-error" title={st.error}>
                                ✗ {st.error && (st.error.substring(0, 24) + (st.error.length > 24 ? '…' : ''))}
                              </span>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>

                <div className="file-list-footer">
                  <button className="btn btn-ghost" onClick={handleClearFiles} disabled={isProcessing}>
                    清空列表
                  </button>
                  <div className="footer-right">
                    {processingCount > 0 && <span className="processing-hint">解密中 {overallProgress.current}/{overallProgress.total}</span>}
                    {successCount > 0 && <span className="success-count">✓ 成功 {successCount}</span>}
                    {errorCount > 0 && <span className="error-count">✗ 失败 {errorCount}</span>}
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* 操作栏 */}
          <div className="action-bar">
            {overallProgress.total > 0 && (
              <div className="progress-wrapper">
                <div className="progress-bar">
                  <div className="progress-fill" style={{width: `${pct}%`}} />
                </div>
                <span className="progress-text">
                  {processingCount > 0
                    ? `正在解密 ${overallProgress.current}/${overallProgress.total}`
                    : pct >= 100 ? '解密完成' : `${pct}%`}
                </span>
              </div>
            )}
            <button
              className={`btn btn-large ${isProcessing ? 'btn-disabled' : 'btn-primary'}`}
              onClick={handleDecrypt}
              disabled={isProcessing || files.length === 0 || !outputDir}
            >
              {isProcessing ? '解密中...' : '开始解密'}
            </button>
          </div>
        </main>

        {/* ========== 详情面板 ========== */}
        {detailInfo && (
          <aside className="detail-panel">
            <div className="detail-header">
              <h2 className="detail-title">歌曲详情</h2>
              <button className="btn-close" onClick={handleCloseDetail} title="关闭">✕</button>
            </div>

            <div className="detail-body">
              {/* 封面图片 */}
              <div className="cover-section">
                {coverLoading ? (
                  <div className="cover-placeholder">加载中...</div>
                ) : detailInfo.coverDataUrl ? (
                  <img
                    className="cover-image"
                    src={detailInfo.coverDataUrl}
                    alt={`${detailInfo.title} 封面`}
                  />
                ) : (
                  <div className="cover-placeholder">
                    <span className="cover-placeholder-icon">🎵</span>
                    <span>无封面</span>
                  </div>
                )}
              </div>

              {/* 元数据 */}
              <div className="meta-section">
                <div className="meta-item">
                  <span className="meta-label">歌曲</span>
                  <span className="meta-value">{detailInfo.title || '未知'}</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">歌手</span>
                  <span className="meta-value">{detailInfo.artist || '未知'}</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">专辑</span>
                  <span className="meta-value">{detailInfo.album || '未知'}</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">格式</span>
                  <span className="meta-value format-badge">{detailInfo.format.toUpperCase()}</span>
                </div>
              </div>

              {/* 文件信息 */}
              <div className="file-section">
                <h3 className="section-title">输出文件</h3>
                <div className="meta-item">
                  <span className="meta-label">文件名</span>
                  <span className="meta-value file-path" title={detailInfo.output}>
                    {detailInfo.output.split('\\').pop()?.split('/').pop() || detailInfo.output}
                  </span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">路径</span>
                  <span className="meta-value file-path" title={detailInfo.output}>
                    {detailInfo.output}
                  </span>
                </div>
                {detailInfo.coverPath && (
                  <div className="meta-item">
                    <span className="meta-label">封面</span>
                    <span className="meta-value file-path" title={detailInfo.coverPath}>
                      {detailInfo.coverPath.split('\\').pop()?.split('/').pop()}
                    </span>
                  </div>
                )}
              </div>
            </div>
          </aside>
        )}
      </div>

      {/* ========== 底部状态栏 ========== */}
      <footer className="app-footer">
        <span>支持拖拽 .ncm 文件到窗口</span>
        <span>成功文件点击查看详情</span>
        <span className="footer-dir" title={outputDir}>输出: {outputDir}</span>
      </footer>
    </div>
  );
}

export default App;
