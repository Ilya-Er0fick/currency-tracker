import asyncio
import os
import signal

from collector.__main__ import install_stop_handlers

from .helpers import RecordingLogger


def test_stop_handlers_fallback_without_loop_signal_support(monkeypatch):
    """Запасной путь для Windows, где add_signal_handler не поддерживается."""
    previous = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
    logger = RecordingLogger()

    async def go(sig):
        loop = asyncio.get_running_loop()

        def unsupported(*args):
            raise NotImplementedError

        monkeypatch.setattr(loop, "add_signal_handler", unsupported)
        stop = asyncio.Event()
        install_stop_handlers(loop, stop, logger)
        os.kill(os.getpid(), sig)
        await asyncio.wait_for(stop.wait(), 1)

    try:
        for sig in (signal.SIGINT, signal.SIGTERM):
            asyncio.run(go(sig))
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)

    # Каждый обработчик сообщает свой сигнал, а не последний из цикла.
    reported = [f["signal"] for _, msg, f in logger.records if msg == "collector shutting down"]
    assert reported == ["SIGINT", "SIGTERM"]
