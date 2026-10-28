"""Предохранитель (Circuit Breaker) для одного источника."""

from __future__ import annotations

import enum
import time
from typing import Callable

from .log import Logger


class State(enum.Enum):
    CLOSED = "closed"
    OPEN = "open"
    HALF_OPEN = "half-open"


class CircuitBreaker:
    """Приостанавливает обращения к источнику после серии неуспешных циклов.

    Closed: опрос идёт по расписанию. После threshold неуспешных циклов
    подряд — Open: обращения приостановлены на open_interval секунд.
    По истечении — Half-open: допускается один пробный запрос, успех
    возвращает в Closed, отказ снова в Open.
    """

    def __init__(self, name: str, threshold: int, open_interval: float, logger: Logger,
                 clock: Callable[[], float] = time.monotonic):
        self.name = name
        self._threshold = threshold
        self._open_interval = open_interval
        self._logger = logger
        self._clock = clock
        self._state = State.CLOSED
        self._failures = 0
        self._opened_at = 0.0

    @property
    def state(self) -> State:
        # Переход в Half-open вычисляется при чтении: отдельный таймер
        # для него не нужен, состояние спрашивают перед каждым циклом.
        if self._state is State.OPEN and self._clock() - self._opened_at >= self._open_interval:
            self._transition(State.HALF_OPEN)
        return self._state

    def allow(self) -> bool:
        return self.state is not State.OPEN

    def record_success(self) -> None:
        self._failures = 0
        if self._state is not State.CLOSED:
            self._transition(State.CLOSED)

    def record_failure(self) -> None:
        if self.state is State.HALF_OPEN:
            self._open()
            return
        self._failures += 1
        if self._state is State.CLOSED and self._failures >= self._threshold:
            self._open()

    def _open(self) -> None:
        self._opened_at = self._clock()
        self._transition(State.OPEN)

    def _transition(self, state: State) -> None:
        self._logger.warning("circuit breaker state changed", source=self.name,
                             from_state=self._state.value, to_state=state.value,
                             failures=self._failures)
        self._state = state
