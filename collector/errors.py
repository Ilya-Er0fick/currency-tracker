"""Виды отказов внешних источников."""


class SourceError(Exception):
    """Источник не дал пригодного ответа."""


class TransientError(SourceError):
    """Таймаут, разрыв соединения, 429 или 5xx: сбой на стороне источника."""


class InvalidResponseError(SourceError):
    """Ответ получен, но разобрать его нельзя."""


class RequestRejectedError(SourceError):
    """Источник отклонил запрос (4xx): ошибка в запросе."""
